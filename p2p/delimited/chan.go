package delimited

import (
	"github.com/spacemeshos/go-spacemesh/p2p/net/wire"
	"io"
)

// Chan is a delimited duplex channel. It is used to have a channel interface
// around a delimited.Reader or Writer.
type Chan struct {
	OutMsgChan chan wire.OutMessage
	InMsgChan  chan wire.InMessage
	CloseChan  chan struct{}
}

// Satisfy formatter.

func (s *Chan) In() chan wire.InMessage {
	return s.InMsgChan
}

func (s *Chan) Out() chan wire.OutMessage {
	return s.OutMsgChan
}

type OutMessage struct {
	m []byte
	r chan error
}

func (om OutMessage) Message() []byte {
	return om.m
}

func (om OutMessage) Result() chan error {
	return om.r
}

type InMessage struct {
	m []byte
	e error
}

func (im InMessage) Message() []byte {
	return im.m
}

func (im InMessage) Error() error {
	return im.e
}

func (s *Chan) MakeIn(m []byte, e error) wire.InMessage {
	return InMessage{m, e}
}

func (s *Chan) MakeOut(m []byte, e chan error) wire.OutMessage {
	return OutMessage{m, e}
}

// NewChan constructs a Chan with a given buffer size.
func NewChan(chanSize int) *Chan {
	return &Chan{
		OutMsgChan: make(chan wire.OutMessage, chanSize),
		InMsgChan:  make(chan wire.InMessage, chanSize),
		CloseChan:  make(chan struct{}, 2),
	}
}

func (s *Chan) Pipe(closer io.ReadWriteCloser) {
	go s.ReadFromReader(closer)
	go s.WriteToWriter(closer)
}

// ReadFromReader wraps the given io.Reader with a delimited.Reader, reads all
// messages, ands sends them down the channel.
func (s *Chan) ReadFromReader(r io.Reader) {
	s.readFrom(NewReader(r))
}

// ReadFrom wraps the given io.Reader with a delimited.Reader, reads all
// messages, ands sends them down the channel.
func (s *Chan) readFrom(mr *Reader) {
	// single reader, no need for Mutex
Loop:
	for {
		buf, err := mr.Next()
		if err != nil {
			if err == io.EOF {
				break Loop // done
			}
			// unexpected error. tell the client.
			s.InMsgChan <- InMessage{nil, err}
			break Loop
		}

		select {
		case <-s.CloseChan:
			break Loop // told we're done
		default:
			if buf != nil {
				newbuf := make([]byte, len(buf))
				copy(newbuf, buf)
				// ok seems fine. send it away
				s.InMsgChan <- InMessage{newbuf, nil}
			}
		}
	}

	close(s.InMsgChan)
	// signal we're done
	s.CloseChan <- struct{}{}
}

// WriteToWriter wraps the given io.Writer with a delimited.Writer, listens on the
// channel and writes all messages to the writer.
func (s *Chan) WriteToWriter(w io.Writer) {
	// new buffer per message
	// if bottleneck, cycle around a set of buffers
	mw := NewWriter(w)

	// single writer, no need for Mutex
Loop:
	for {
		select {
		case <-s.CloseChan:
			break Loop // told we're done

		case msg, ok := <-s.OutMsgChan:
			if !ok { // chan closed
				break Loop
			}

			if _, err := mw.WriteRecord(msg.Message()); err != nil {
				if err != io.EOF {
					// unexpected error. tell the client.
					msg.Result() <- err
				}

				break Loop
			}
			// Report msg was sent
			msg.Result() <- nil
		}
	}

	// signal we're done
	s.CloseChan <- struct{}{}
}

// Close the Chan
func (s *Chan) Close() {
	s.CloseChan <- struct{}{}
}
