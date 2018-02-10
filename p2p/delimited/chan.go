package delimited

import (
	"io"
)

// Chan is a delimited duplex channel. It is used to have a channel interface
// around a delimited.Reader or Writer.
type Chan struct {
	MsgChan   chan []byte
	ErrChan   chan error
	CloseChan chan bool
}

// NewChan constructs a Chan with a given buffer size.
func NewChan(chanSize int) *Chan {
	return &Chan{
		MsgChan:   make(chan []byte, chanSize),
		ErrChan:   make(chan error, 1),
		CloseChan: make(chan bool, 2),
	}
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
			s.ErrChan <- err
			break Loop
		}

		select {
		case <-s.CloseChan:
			break Loop // told we're done
		case s.MsgChan <- buf:
			// ok seems fine. send it away
		}
	}

	close(s.MsgChan)
	// signal we're done
	s.CloseChan <- true
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

		case msg, ok := <-s.MsgChan:
			if !ok { // chan closed
				break Loop
			}

			if _, err := mw.WriteRecord(msg); err != nil {
				if err != io.EOF {
					// unexpected error. tell the client.
					s.ErrChan <- err
				}

				break Loop
			}
		}
	}

	// signal we're done
	s.CloseChan <- true
}

// Close the Chan
func (s *Chan) Close() {
	s.CloseChan <- true
}
