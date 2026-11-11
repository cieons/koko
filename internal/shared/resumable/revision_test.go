package resumable

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"testing"

	"github.com/cieons/koko/internal/harness/frozen"
	"github.com/cieons/koko/internal/shared/wire"
)

var update = flag.Bool("update", false, "add new revisions to testdata/frames.golden")

// The stream's frames and its answer to a resume are recorded for every
// revision of tunnel@koko, which covers them, and a recorded revision never
// changes: a change to a frame's encoding or a new frame type without a
// raise fails here, and -update records only a new revision (ADR 0049).
// What the decoder reads of each frame type is recorded too. The stream
// speaks one encoding, so every revision in the record must be it until
// the stream takes a revision of its own.
func TestTheFramesAreRecordedByRevision(t *testing.T) {
	t.Parallel()
	const ack = 70000 // three bytes as a uvarint
	frames := fmt.Sprintf("data, ack %d, \"hi\": % x\neof, ack %d: % x\nclose, ack %d, \"bye\": % x\nresume answer, offset %d: % x",
		ack, appendData(nil, ack, []byte("hi")),
		ack, appendEOF(nil, ack),
		ack, appendClose(nil, ack, "bye"),
		ack, appendOffset(nil, ack))
	// What the decoder makes of each size past the data's, so a new frame
	// type fails too.
	for size := uint64(MaxData + 1); size <= MaxData+3; size++ {
		frame := binary.AppendUvarint(binary.AppendUvarint(nil, 0), size)
		frames += fmt.Sprintf("\nreads size %d as: %s", size, decoded(append(frame, 0)))
	}
	var sections []frozen.Section
	for rev := 1; rev <= wire.TunnelRevision; rev++ {
		sections = append(sections, frozen.Section{Name: fmt.Sprintf("%s %d", wire.ChannelTunnel, rev), Text: frames})
	}
	frozen.Check(t, "frames.golden", sections, *update)
}

// decoded names the frame readFrame reads from b, or says it is malformed.
func decoded(b []byte) string {
	f, err := readFrame(&byteReader{Reader: bufio.NewReader(bytes.NewReader(b))}, make([]byte, MaxData))
	switch {
	case errors.Is(err, ErrMalformed):
		return "malformed"
	case err != nil:
		return err.Error()
	}
	return [...]string{kindData: "data", kindEOF: "eof", kindClose: "close"}[f.kind]
}
