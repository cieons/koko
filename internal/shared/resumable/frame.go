package resumable

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ErrMalformed is the cause of a stream's end when the other end sent bytes
// that are no frame or resume answer, or a frame its state forbids.
var ErrMalformed = errors.New("malformed frame")

// A frame's size field is a data length up to MaxData, or one of these frame
// types; docs/subsystems/resumable-stream.md has the whole layout.
const (
	sizeEOF   = MaxData + 1
	sizeClose = MaxData + 2
)

// maxFrameLen is the longest frame: a close whose reason is MaxData bytes.
const maxFrameLen = 3*binary.MaxVarintLen64 + MaxData

// kind is what a frame carries.
type kind int

const (
	kindData kind = iota
	kindEOF
	kindClose
)

// frame is one decoded frame; data aliases the reader's scratch buffer.
type frame struct {
	ack    uint64
	kind   kind
	data   []byte // a data frame's
	reason string // a close frame's
}

// appendData appends a data frame carrying ack and data to buf.
func appendData(buf []byte, ack uint64, data []byte) []byte {
	buf = binary.AppendUvarint(buf, ack)
	buf = binary.AppendUvarint(buf, uint64(len(data)))
	return append(buf, data...)
}

// appendEOF appends an EOF frame carrying ack to buf.
func appendEOF(buf []byte, ack uint64) []byte {
	buf = binary.AppendUvarint(buf, ack)
	return binary.AppendUvarint(buf, sizeEOF)
}

// appendClose appends a close frame carrying ack and reason, at most MaxData
// bytes, to buf.
func appendClose(buf []byte, ack uint64, reason string) []byte {
	buf = binary.AppendUvarint(buf, ack)
	buf = binary.AppendUvarint(buf, sizeClose)
	buf = binary.AppendUvarint(buf, uint64(len(reason)))
	return append(buf, reason...)
}

// readFrame reads one frame from r into scratch, which holds MaxData bytes.
// The channel's errors come back as they are, a broken frame as ErrMalformed.
func readFrame(r *byteReader, scratch []byte) (frame, error) {
	ack, err := r.uvarint("an acknowledgement")
	if err != nil {
		return frame{}, err
	}
	size, err := r.uvarint("a size")
	if err != nil {
		return frame{}, err
	}
	f, n := frame{ack: ack}, size
	switch {
	case size == sizeEOF:
		f.kind = kindEOF
		return f, nil
	case size == sizeClose:
		f.kind = kindClose
		if n, err = r.uvarint("a reason's length"); err != nil {
			return frame{}, err
		}
		if n > MaxData {
			return frame{}, fmt.Errorf("%w: a reason of %d bytes, more than %d", ErrMalformed, n, MaxData)
		}
	case size > MaxData:
		return frame{}, fmt.Errorf("%w: size %d, neither data of at most %d bytes nor a frame type", ErrMalformed, size, MaxData)
	}
	f.data = scratch[:n]
	if _, err := io.ReadFull(r, f.data); err != nil {
		return frame{}, err
	}
	if f.kind == kindClose {
		f.reason, f.data = string(f.data), nil
	}
	return f, nil
}

// byteReader reads a channel through a buffer and tells a uvarint that
// overflows, which is malformed, from a channel that fails.
type byteReader struct {
	*bufio.Reader
	err error // the channel's error during the last uvarint
}

func (r *byteReader) ReadByte() (byte, error) {
	b, err := r.Reader.ReadByte()
	if err != nil {
		r.err = err
	}
	return b, err
}

// uvarint reads one uvarint; what names it in an error.
func (r *byteReader) uvarint(what string) (uint64, error) {
	r.err = nil
	v, err := binary.ReadUvarint(r)
	if err != nil && r.err == nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrMalformed, what, err)
	}
	return v, err
}
