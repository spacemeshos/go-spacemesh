package wire

import "io"

// TODO: A sync formatter

type Formatter interface {
	Pipe(rw io.ReadWriteCloser)
	In() chan []byte
	Out(message []byte) error
	Close()
}
