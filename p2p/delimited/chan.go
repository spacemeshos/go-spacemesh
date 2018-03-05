package delimited

import (
	"io"
)

// Chan is a delimited duplex channel. It is used to have a channel interface
// around a delimited.Reader or Writer.
type Chan struct {
	MsgChan     chan []byte
	ErrChan     chan error
	CloseChan   chan bool
	opCallbacks []func()
}

// NewChan constructs a Chan with a given buffer size.
func NewChan(chanSize int) *Chan {
	return &Chan{
		MsgChan:   make(chan []byte, chanSize),
		ErrChan:   make(chan error, 1),
		CloseChan: make(chan bool, 2),
	}
}

func (s *Chan) RegisterOpCallback(f func()) {
	if f != nil {
		s.opCallbacks = append(s.opCallbacks, f)
	}
}

func (s *Chan) runOps() {
	for _, f := range s.opCallbacks {
		f()
	}
}

// ReadFromReader wraps the given io.Reader with a delimited.Reader, reads all
// messages, ands sends them down the channel.
func (s *Chan) ReadFromReader(r io.Reader) {
	s.readFrom(NewReader(r))
}

func (s *Chan) readFrom(dr *Reader) {
	// single reader, no need for Mutex
Loop:
	for {
		buf, err := dr.Next()
		if err != nil {
			if err != io.EOF {
				// unexpected error. tell the client.
				s.ErrChan <- err
				break Loop // done
			}
		}

		select {
		case <-s.CloseChan:
			break Loop // told we're done
		default:
			//pass a copy of buf so we can keep reading to it
			bufcpy := make([]byte, len(buf))
			copy(bufcpy, buf)
			s.MsgChan <- bufcpy
			s.runOps()
		}
	}

	close(s.MsgChan)
	// signal we're done
	s.CloseChan <- true
}

// WriteToWriter wraps the given io.Writer with a delimited.Writer, listens on the
// channel and writes all messages to the writer.
func (s *Chan) WriteToWriter(w io.Writer) {
	s.writeTo(NewWriter(w))
}

func (s *Chan) writeTo(dw *Writer) {
	// new buffer per message
	// if bottleneck, cycle around a set of buffers
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

			if _, err := dw.WriteRecord(msg); err != nil {
				if err != io.EOF {
					// unexpected error. tell the client.
					s.ErrChan <- err
				}

				break Loop
			}
			s.runOps()
		}
	}

	// signal we're done
	s.CloseChan <- true
}

// Close the Chan
func (s *Chan) Close() {
	s.CloseChan <- true
}
