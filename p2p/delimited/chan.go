package delimited

import (
	"fmt"
	"io"
	"sync"
)

// Chan is a delimited duplex channel. It is used to have a channel interface
// around a delimited.Reader or Writer.
type Chan struct {
	connection io.ReadWriteCloser
	closeOnce  sync.Once

	outMsgChan chan outMessage
	inMsgChan  chan []byte
	CloseChan  chan struct{}
}

// Satisfy formatter.

// In exposes the incoming message channel
func (s *Chan) In() chan []byte {
	return s.inMsgChan
}

// Out sends message on the wire, blocking.
func (s *Chan) Out(message []byte) error {
	outCb := make(chan error)
	select {
	case s.outMsgChan <- outMessage{message, outCb}:
		return <-outCb
	case <-s.CloseChan:
		return fmt.Errorf("formatter is closed")
	}
}

type outMessage struct {
	m []byte
	r chan error
}

func (om outMessage) Message() []byte {
	return om.m
}

func (om outMessage) Result() chan error {
	return om.r
}

// NewChan constructs a Chan with a given buffer size.
func NewChan(chanSize int) *Chan {
	return &Chan{
		outMsgChan: make(chan outMessage, chanSize),
		inMsgChan:  make(chan []byte, chanSize),
		CloseChan:  make(chan struct{}),
	}
}

// Pipe invokes the reader and writer flows, once it's ran Chan can start serving incoming/outgoing messages
func (s *Chan) Pipe(rwc io.ReadWriteCloser) {
	s.connection = rwc
	go s.readFromReader(rwc)
	go s.writeToWriter(rwc)
}

// ReadFrom wraps the given io.Reader with a delimited.Reader, reads all
// messages, ands sends them down the channel.
func (s *Chan) readFromReader(r io.Reader) {

	mr := NewReader(r)
	// single reader, no need for Mutex
Loop:
	for {
		buf, err := mr.Next()
		if err != nil {
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
				s.inMsgChan <- newbuf
			}
		}
	}

	s.Close() // close writer
	close(s.inMsgChan)
}

// WriteToWriter wraps the given io.Writer with a delimited.Writer, listens on the
// channel and writes all messages to the writer.
func (s *Chan) writeToWriter(w io.Writer) {
	// new buffer per message
	// if bottleneck, cycle around a set of buffers
	mw := NewWriter(w)

	// single writer, no need for Mutex
Loop:
	for {
		select {
		case <-s.CloseChan:
			break Loop // told we're done

		case msg := <-s.outMsgChan:
			if _, err := mw.WriteRecord(msg.Message()); err != nil {
				// unexpected error. tell the client.
				msg.Result() <- err
			} else {
				// Report msg was sent
				msg.Result() <- nil
			}
		}
	}
}

// Close the Chan
func (s *Chan) Close() {
	s.closeOnce.Do(func() {
		close(s.CloseChan)   // close both writer and reader
		s.connection.Close() // close internal connection
	})
}
