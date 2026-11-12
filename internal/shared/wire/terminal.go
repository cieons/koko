package wire

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// TerminalProtocol is the protocol a web terminal's page and the koko
// server speak: the first messages, the records below and the resumable
// stream's frames as the terminal uses them. It rises with any change to
// them; the page and the koko server are one build, so the koko server
// takes only its own (P1 spec: a terminal in the web UI, "Revisions").
const TerminalProtocol = 1

// TerminalMaxRecord is the most bytes one terminal record's payload holds.
const TerminalMaxRecord = 32 << 10

// TerminalMaxSide is the most columns, and the most rows, a resize record
// carries; the least is 1.
const TerminalMaxSide = 4096

// TerminalRecordType is the type of a terminal record: its first byte.
type TerminalRecordType byte

// The terminal records. A web terminal's resumable stream carries a
// sequence of them each way: a one-byte type, a uvarint length of at most
// TerminalMaxRecord, then a payload of that length.
const (
	// TerminalData carries bytes typed or pasted, from the page, or the
	// shell's output, from the Gateway.
	TerminalData TerminalRecordType = 0x00
	// TerminalResize, from the page, carries the terminal's size: uvarint
	// columns, then uvarint rows, each 1 to TerminalMaxSide.
	TerminalResize TerminalRecordType = 0x01
	// TerminalExitStatus, from the Gateway, carries the shell's exit status
	// as a uvarint, at most a uint32's.
	TerminalExitStatus TerminalRecordType = 0x02
	// TerminalExitSignal, from the Gateway, carries the name of the signal
	// that ended the shell, such as KILL.
	TerminalExitSignal TerminalRecordType = 0x03
)

// TerminalRecord is one terminal record. Only its type's fields are set.
type TerminalRecord struct {
	Type          TerminalRecordType
	Data          []byte // TerminalData's
	Columns, Rows uint32 // TerminalResize's
	Status        uint32 // TerminalExitStatus's
	Signal        string // TerminalExitSignal's
}

// ErrTerminalMalformed is the error for bytes that are no terminal record:
// an unknown type, a length past TerminalMaxRecord, a resize out of range,
// or a payload that its length cuts short or that goes on past its fields.
// The same error refuses to encode such a record.
var ErrTerminalMalformed = errors.New("malformed terminal record")

// AppendTerminalRecord appends r's encoding to buf. A record the decoder
// would refuse fails with ErrTerminalMalformed and leaves buf as it was.
func AppendTerminalRecord(buf []byte, r TerminalRecord) ([]byte, error) {
	var payload []byte
	switch r.Type {
	case TerminalData:
		payload = r.Data
	case TerminalResize:
		if err := checkTerminalSize(uint64(r.Columns), uint64(r.Rows)); err != nil {
			return buf, err
		}
		payload = binary.AppendUvarint(binary.AppendUvarint(nil, uint64(r.Columns)), uint64(r.Rows))
	case TerminalExitStatus:
		payload = binary.AppendUvarint(nil, uint64(r.Status))
	case TerminalExitSignal:
		payload = []byte(r.Signal)
	default:
		return buf, fmt.Errorf("%w: type %#x", ErrTerminalMalformed, byte(r.Type))
	}
	if len(payload) > TerminalMaxRecord {
		return buf, fmt.Errorf("%w: a payload of %d bytes, more than %d", ErrTerminalMalformed, len(payload), TerminalMaxRecord)
	}
	buf = append(buf, byte(r.Type))
	buf = binary.AppendUvarint(buf, uint64(len(payload)))
	return append(buf, payload...), nil
}

// checkTerminalSize refuses a terminal size out of range.
func checkTerminalSize(columns, rows uint64) error {
	if columns < 1 || columns > TerminalMaxSide || rows < 1 || rows > TerminalMaxSide {
		return fmt.Errorf("%w: a resize to %dx%d, each side being 1 to %d", ErrTerminalMalformed, columns, rows, TerminalMaxSide)
	}
	return nil
}

// TerminalReader decodes a stream of terminal records.
type TerminalReader struct {
	r       *bufio.Reader
	payload []byte // the last record's, grown on demand
}

// NewTerminalReader returns a decoder of the records r carries.
func NewTerminalReader(r io.Reader) *TerminalReader {
	return &TerminalReader{r: bufio.NewReader(r)}
}

// Next reads the next record. Its Data is valid until the next call. The
// stream's errors come back as they are, io.EOF only between records and
// io.ErrUnexpectedEOF inside one; bytes that are no record fail with
// ErrTerminalMalformed, after which the stream cannot be read further.
func (d *TerminalReader) Next() (TerminalRecord, error) {
	t, err := d.r.ReadByte()
	if err != nil {
		return TerminalRecord{}, err
	}
	rec := TerminalRecord{Type: TerminalRecordType(t)}
	if rec.Type > TerminalExitSignal {
		return TerminalRecord{}, fmt.Errorf("%w: type %#x", ErrTerminalMalformed, t)
	}
	n, err := d.length()
	if err != nil {
		return TerminalRecord{}, err
	}
	if cap(d.payload) < n {
		d.payload = make([]byte, n)
	}
	payload := d.payload[:n]
	if _, err := io.ReadFull(d.r, payload); err != nil {
		return TerminalRecord{}, insideRecord(err)
	}
	switch rec.Type {
	case TerminalData:
		rec.Data = payload
	case TerminalResize:
		columns, rest, err := terminalUvarint(payload, "columns")
		if err != nil {
			return TerminalRecord{}, err
		}
		rows, rest, err := terminalUvarint(rest, "rows")
		if err != nil {
			return TerminalRecord{}, err
		}
		if err := errors.Join(terminalPayloadEnd(rest), checkTerminalSize(columns, rows)); err != nil {
			return TerminalRecord{}, err
		}
		rec.Columns, rec.Rows = uint32(columns), uint32(rows)
	case TerminalExitStatus:
		status, rest, err := terminalUvarint(payload, "an exit status")
		if err != nil {
			return TerminalRecord{}, err
		}
		if err := terminalPayloadEnd(rest); err != nil {
			return TerminalRecord{}, err
		}
		if status > math.MaxUint32 {
			return TerminalRecord{}, fmt.Errorf("%w: an exit status of %d, more than a uint32 holds", ErrTerminalMalformed, status)
		}
		rec.Status = uint32(status)
	case TerminalExitSignal:
		rec.Signal = string(payload)
	}
	return rec, nil
}

// length reads a record's length.
func (d *TerminalReader) length() (int, error) {
	var readErr error
	n, err := binary.ReadUvarint(byteReaderFunc(func() (byte, error) {
		b, err := d.r.ReadByte()
		readErr = err
		return b, err
	}))
	switch {
	case readErr != nil:
		return 0, insideRecord(readErr)
	case err != nil:
		return 0, fmt.Errorf("%w: a length: %w", ErrTerminalMalformed, err)
	case n > TerminalMaxRecord:
		return 0, fmt.Errorf("%w: a length of %d, more than %d", ErrTerminalMalformed, n, TerminalMaxRecord)
	}
	return int(n), nil
}

// byteReaderFunc is an io.ByteReader that tells its caller of each error.
type byteReaderFunc func() (byte, error)

func (f byteReaderFunc) ReadByte() (byte, error) { return f() }

// insideRecord is the stream's error err when it came inside a record,
// where the stream's end is unexpected.
func insideRecord(err error) error {
	if err == io.EOF { //nolint:errorlint // only the stream's own end, as io.ReadFull tells it
		return io.ErrUnexpectedEOF
	}
	return err
}

// terminalUvarint reads one uvarint, which what names, off a payload's
// front and returns the rest.
func terminalUvarint(payload []byte, what string) (uint64, []byte, error) {
	v, n := binary.Uvarint(payload)
	if n <= 0 {
		return 0, nil, fmt.Errorf("%w: %s cut short by the record's length, or past a uvarint", ErrTerminalMalformed, what)
	}
	return v, payload[n:], nil
}

// terminalPayloadEnd refuses a payload that goes on past its fields.
func terminalPayloadEnd(rest []byte) error {
	if len(rest) > 0 {
		return fmt.Errorf("%w: %d bytes after the payload's fields", ErrTerminalMalformed, len(rest))
	}
	return nil
}
