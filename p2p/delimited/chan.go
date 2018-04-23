package delimited

import (
	"io"
)

// Chan is a delimited duplex channel. It is used to have a channel interface
// around a delimited.Reader or Writer.
type Chan struct {
	MsgChan   chan MsgAndID
	ErrChan   chan ErrAndID
	CloseChan chan struct{}
}

// MsgAndID is a struct for passing a message paired with an id,
// we need the id to report errors with this message id later
// ID is nil on an incoming message
type MsgAndID struct {
	ID  []byte
	Msg []byte
}

// ErrAndID is a struct that holds an error and an id
// it is used to report errors regarding the specific message id.
// ID is nil when the err is incoming error message (we don't know the id)
type ErrAndID struct {
	ID  []byte
	Err error
}

// NewChan constructs a Chan with a given buffer size.
func NewChan(chanSize int) *Chan {
	return &Chan{
		MsgChan:   make(chan MsgAndID, chanSize),
		ErrChan:   make(chan ErrAndID, 1),
		CloseChan: make(chan struct{}, 2),
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
			s.ErrChan <- ErrAndID{nil, err}
			break Loop
		}

		select {
		case <-s.CloseChan:
			break Loop // told we're done
		default:
			if buf != nil {
				emptybuf := make([]byte, len(buf))
				copy(emptybuf, buf)
				// ok seems fine. send it away
				s.MsgChan <- MsgAndID{nil, emptybuf}
			}
		}
	}

	close(s.MsgChan)
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

		case msg, ok := <-s.MsgChan:
			if !ok { // chan closed
				break Loop
			}

			if _, err := mw.WriteRecord(msg.Msg); err != nil {
				if err != io.EOF {
					// unexpected error. tell the client.
					s.ErrChan <- ErrAndID{msg.ID, err}
				}

				break Loop
			}
		}
	}

	// signal we're done
	s.CloseChan <- struct{}{}
}

// Close the Chan
func (s *Chan) Close() {
	s.CloseChan <- struct{}{}
}
