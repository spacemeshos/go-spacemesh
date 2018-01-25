package msgio

import (
	"bytes"
	"io"
	"sync"
)

// LimitedReader wraps an io.Reader with a msgio framed reader. The LimitedReader
// will return a reader which will io.EOF when the msg length is done.
func LimitedReader(r io.Reader) (io.Reader, error) {
	l, err := ReadLen(r, nil)
	return io.LimitReader(r, int64(l)), err
}

// NewLimitedWriter creates a new LimitedWriter.
func NewLimitedWriter(w io.Writer) *LimitedWriter {
	return &LimitedWriter{W: w}
}

// LimitedWriter wraps an io.Writer with a msgio framed writer. It is the inverse
// of LimitedReader: it will buffer all writes until "Flush" is called. When Flush
// is called, it will write the size of the buffer first, flush the buffer, reset
// the buffer, and begin accept more incoming writes.
type LimitedWriter struct {
	W io.Writer
	B bytes.Buffer
	M sync.Mutex
}

func (w *LimitedWriter) Write(buf []byte) (n int, err error) {
	w.M.Lock()
	n, err = w.B.Write(buf)
	w.M.Unlock()
	return n, err
}

// Flush flushes the writer
func (w *LimitedWriter) Flush() error {
	w.M.Lock()
	defer w.M.Unlock()
	if err := WriteLen(w.W, w.B.Len()); err != nil {
		return err
	}
	_, err := w.B.WriteTo(w.W)
	return err
}
