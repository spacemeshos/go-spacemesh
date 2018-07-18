package wire

import "io"

// TODO: A sync formatter

type Formatter interface {
	Pipe(rw io.ReadWriter)
	In() chan InMessage
	Out() chan OutMessage
	MakeIn(m []byte, e error) InMessage
	MakeOut(m []byte, e chan error) OutMessage
	Close()
}

func Send(f Formatter, m []byte) error {
	err := make(chan error)
	f.Out() <- f.MakeOut(m, err)
	return <-err
}

type InMessage interface {
	Message() []byte
	Error() error
}

type OutMessage interface {
	Message() []byte
	Result() chan error
}
