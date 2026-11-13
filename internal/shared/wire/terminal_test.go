package wire_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness/frozen"
	"github.com/cieons/koko/internal/shared/wire"
)

// The records every web terminal protocol carries are recorded, with what
// the decoder reads from each and from each malformed one, and a recorded
// protocol never changes: a change to a record's encoding or its reading
// without a raise of wire.TerminalProtocol fails here, and -update records
// only a new protocol. The TypeScript end's test reads the same file.
func TestTheTerminalRecordsAreRecordedByProtocol(t *testing.T) {
	t.Parallel()
	type malformed struct {
		what  string
		bytes []byte
	}
	// Protocol 1's records, and the bytes it read as no record.
	records := []wire.TerminalRecord{
		{Type: wire.TerminalData, Data: []byte("hi")},
		{Type: wire.TerminalData},
		{Type: wire.TerminalResize, Columns: 80, Rows: 24},
		{Type: wire.TerminalResize, Columns: 1, Rows: 1},
		{Type: wire.TerminalResize, Columns: 4096, Rows: 4096},
		{Type: wire.TerminalExitStatus, Status: 0},
		{Type: wire.TerminalExitStatus, Status: 255},
		{Type: wire.TerminalExitStatus, Status: 4294967295},
		{Type: wire.TerminalExitSignal, Signal: "KILL"},
	}
	bad := []malformed{
		{"a length of 32769", []byte{0x00, 0x81, 0x80, 0x02}},
		{"a length past a uvarint", []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}},
		{"a resize of 0 columns", []byte{0x01, 0x02, 0x00, 0x18}},
		{"a resize of 0 rows", []byte{0x01, 0x02, 0x50, 0x00}},
		{"a resize of 4097 columns", []byte{0x01, 0x03, 0x81, 0x20, 0x18}},
		{"a resize of 4097 rows", []byte{0x01, 0x03, 0x50, 0x81, 0x20}},
		{"a resize without rows", []byte{0x01, 0x01, 0x50}},
		{"a resize whose rows end past its length", []byte{0x01, 0x02, 0x50, 0x81}},
		{"a resize with a byte after its rows", []byte{0x01, 0x03, 0x50, 0x18, 0x00}},
		{"an exit status without a status", []byte{0x02, 0x00}},
		{"an exit status of 4294967296", []byte{0x02, 0x05, 0x80, 0x80, 0x80, 0x80, 0x10}},
		{"an exit status with a byte after it", []byte{0x02, 0x02, 0x00, 0x00}},
	}
	byProtocol := map[int]struct {
		records []wire.TerminalRecord
		bad     []malformed
	}{
		1: {records, append([]malformed{{"an unknown type", []byte{0x04, 0x00}}}, bad...)},
		// Protocol 2 attaches a page to a Workspace's SSH Session.
		2: {
			append(slices.Clone(records),
				wire.TerminalRecord{Type: wire.TerminalReset, Columns: 80, Rows: 24},
				wire.TerminalRecord{Type: wire.TerminalReset, Columns: 500, Rows: 200},
				wire.TerminalRecord{Type: wire.TerminalActive, Activity: wire.TerminalActed},
				wire.TerminalRecord{Type: wire.TerminalActive, Activity: wire.TerminalNowActive},
				wire.TerminalRecord{Type: wire.TerminalActive, Activity: wire.TerminalNoLongerActive},
				wire.TerminalRecord{Type: wire.TerminalColors, Foreground: [3]byte{0x0c, 0x0c, 0x0c}, Background: [3]byte{0xff, 0xff, 0xff}},
				wire.TerminalRecord{Type: wire.TerminalDisplaced},
			),
			append(append([]malformed{{"an unknown type", []byte{0x08, 0x00}}}, bad...),
				malformed{"a reset of 0 columns", []byte{0x04, 0x02, 0x00, 0x18}},
				malformed{"a reset of 4097 rows", []byte{0x04, 0x03, 0x50, 0x81, 0x20}},
				malformed{"a reset without rows", []byte{0x04, 0x01, 0x50}},
				malformed{"an active record of 2", []byte{0x05, 0x01, 0x02}},
				malformed{"an active record of 2 bytes", []byte{0x05, 0x02, 0x01, 0x00}},
				malformed{"colours of 5 bytes", []byte{0x06, 0x05, 0x0c, 0x0c, 0x0c, 0xff, 0xff}},
				malformed{"colours of 7 bytes", []byte{0x06, 0x07, 0x0c, 0x0c, 0x0c, 0xff, 0xff, 0xff, 0x00}},
				malformed{"a displaced record with a byte", []byte{0x07, 0x01, 0x00}},
			),
		},
	}
	var sections []frozen.Section
	for protocol := 1; protocol <= wire.TerminalProtocol; protocol++ {
		p, ok := byProtocol[protocol]
		if !ok {
			t.Fatalf("protocol %d lists no records", protocol)
		}
		var lines []string
		for _, r := range p.records {
			b, err := wire.AppendTerminalRecord(nil, r)
			if err != nil {
				t.Fatalf("encode %s: %v", described(r, nil), err)
			}
			lines = append(lines, fmt.Sprintf("%s: % x\n  read as: %s", described(r, nil), b, decoded(b)))
		}
		for _, m := range p.bad {
			lines = append(lines, fmt.Sprintf("%s: % x\n  read as: %s", m.what, m.bytes, decoded(m.bytes)))
		}
		sections = append(sections, frozen.Section{Name: fmt.Sprintf("terminal %d", protocol), Text: strings.Join(lines, "\n")})
	}
	frozen.Check(t, "terminal.golden", sections, *update)
}

// described names a record as the golden file does, or says it is malformed.
func described(r wire.TerminalRecord, err error) string {
	switch {
	case errors.Is(err, wire.ErrTerminalMalformed):
		return "malformed"
	case err != nil:
		return err.Error()
	}
	switch r.Type {
	case wire.TerminalData:
		return fmt.Sprintf("data %q", r.Data)
	case wire.TerminalResize:
		return fmt.Sprintf("resize %dx%d", r.Columns, r.Rows)
	case wire.TerminalExitStatus:
		return fmt.Sprintf("exit status %d", r.Status)
	case wire.TerminalExitSignal:
		return "exit signal " + r.Signal
	case wire.TerminalReset:
		return fmt.Sprintf("reset %dx%d", r.Columns, r.Rows)
	case wire.TerminalActive:
		switch r.Activity {
		case wire.TerminalNowActive:
			return "active 1"
		case wire.TerminalNoLongerActive:
			return "active 0"
		}
		return "active"
	case wire.TerminalColors:
		return fmt.Sprintf("colors %x %x", r.Foreground, r.Background)
	case wire.TerminalDisplaced:
		return "displaced"
	}
	return fmt.Sprintf("type %#x", byte(r.Type))
}

// decoded says what the decoder reads from b, which holds one record.
func decoded(b []byte) string {
	return described(wire.NewTerminalReader(bytes.NewReader(b)).Next())
}

// A stream of records decodes to the records written, in order, however
// the stream hands its bytes over, and then ends as the stream does.
func TestAStreamOfTerminalRecordsDecodesInOrder(t *testing.T) {
	t.Parallel()
	want := []wire.TerminalRecord{
		{Type: wire.TerminalResize, Columns: 120, Rows: 40},
		{Type: wire.TerminalData, Data: bytes.Repeat([]byte{'x'}, wire.TerminalMaxRecord)},
		{Type: wire.TerminalData, Data: []byte("exit\r\n")},
		{Type: wire.TerminalExitSignal, Signal: "TERM"},
		{Type: wire.TerminalExitStatus, Status: 130},
	}
	var stream []byte
	for _, r := range want {
		var err error
		if stream, err = wire.AppendTerminalRecord(stream, r); err != nil {
			t.Fatalf("encode %s: %v", described(r, nil), err)
		}
	}
	for name, r := range map[string]io.Reader{
		"whole":          bytes.NewReader(stream),
		"a byte a read":  iotestOneByte{bytes.NewReader(stream)},
		"cut mid-record": bytes.NewReader(stream[:len(stream)-1]),
	} {
		dec := wire.NewTerminalReader(r)
		for i, w := range want {
			got, err := dec.Next()
			if name == "cut mid-record" && i == len(want)-1 {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Errorf("%s: a stream that ends inside a record reads as %v, want io.ErrUnexpectedEOF", name, err)
				}
				break
			}
			if err != nil {
				t.Fatalf("%s: record %d: %v", name, i, err)
			}
			if g, w := described(got, nil), described(w, nil); g != w {
				t.Errorf("%s: record %d reads as %s, want %s", name, i, g, w)
			}
		}
		if name == "cut mid-record" {
			continue
		}
		if _, err := dec.Next(); !errors.Is(err, io.EOF) || errors.Is(err, wire.ErrTerminalMalformed) {
			t.Errorf("%s: after the last record: %v, want io.EOF", name, err)
		}
	}
}

// iotestOneByte hands its reader's bytes over one at a time.
type iotestOneByte struct{ r io.Reader }

func (o iotestOneByte) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}

// A record the decoder would refuse is not encoded.
func TestARecordOutOfRangeIsNotEncoded(t *testing.T) {
	t.Parallel()
	for _, r := range []wire.TerminalRecord{
		{Type: wire.TerminalData, Data: make([]byte, wire.TerminalMaxRecord+1)},
		{Type: wire.TerminalExitSignal, Signal: strings.Repeat("x", wire.TerminalMaxRecord+1)},
		{Type: wire.TerminalResize, Columns: 0, Rows: 24},
		{Type: wire.TerminalResize, Columns: 80, Rows: 0},
		{Type: wire.TerminalResize, Columns: 4097, Rows: 24},
		{Type: wire.TerminalResize, Columns: 80, Rows: 4097},
		{Type: wire.TerminalReset, Columns: 0, Rows: 24},
		{Type: wire.TerminalActive, Activity: 3},
		{Type: 0x08},
	} {
		prefix := []byte("kept")
		got, err := wire.AppendTerminalRecord(prefix, r)
		if !errors.Is(err, wire.ErrTerminalMalformed) {
			t.Errorf("encoding %s: %v, want ErrTerminalMalformed", described(r, nil), err)
		}
		if string(got) != "kept" {
			t.Errorf("encoding %s left %q in the buffer, want what was there", described(r, nil), got)
		}
	}
}
