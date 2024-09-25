package rangesync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
)

const (
	writeQueueSize = 10000
)

var ErrLimitExceeded = errors.New("sync traffic/message limit exceeded")

// ConduitOption specifies an option for a message conduit.
type ConduitOption func(c *wireConduit)

// WithTrafficLimit sets a limit on the total number of bytes sent and received.
// Zero or negative values disable the limit.
func WithTrafficLimit(limit int) ConduitOption {
	return func(c *wireConduit) {
		c.trafficLimit = limit
	}
}

// WithMessageLimit sets a limit on the total number of messages sent and received.
// Zero or negative values disable the limit.
func WithMessageLimit(limit int) ConduitOption {
	return func(c *wireConduit) {
		c.messageLimit = limit
	}
}

type wireConduit struct {
	stream       io.ReadWriter
	eg           errgroup.Group
	sendCh       chan SyncMessage
	stopCh       chan struct{}
	nBytesSent   atomic.Int64
	nBytesRecv   atomic.Int64
	nMsgsSent    atomic.Int64
	nMsgsRecv    atomic.Int64
	trafficLimit int
	messageLimit int
}

var _ Conduit = &wireConduit{}

func startWireConduit(ctx context.Context, s io.ReadWriter, opts ...ConduitOption) *wireConduit {
	c := &wireConduit{
		stream: s,
		sendCh: make(chan SyncMessage, writeQueueSize),
		stopCh: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}
	c.eg.Go(func() error {
		defer close(c.stopCh)
		for {
			select {
			case <-ctx.Done():
				c.closeStream()
				return ctx.Err()
			case m, ok := <-c.sendCh:
				if !ok {
					return nil
				}
				n, err := writeMessage(c.stream, m)
				c.nBytesSent.Add(int64(n))
				c.nMsgsSent.Add(1)
				if err == nil {
					err = c.checkLimits()
				}
				if err != nil {
					c.closeStream()
					return err
				}
			}
		}
	})
	return c
}

func (c *wireConduit) closeStream() {
	if closer, ok := c.stream.(io.Closer); ok {
		closer.Close()
	}
}

func (c *wireConduit) stop() {
	if c.stream == nil {
		return
	}
	// if there was in error, there's no point in waiting for the send
	// goroutine to finish, so we interrupt it by closing the stream
	c.closeStream()
	c.end()
}

func (c *wireConduit) end() {
	if c.stream == nil {
		return
	}
	close(c.sendCh)
	c.eg.Wait()
	c.stream = nil
}

func (c *wireConduit) checkLimits() error {
	if c.trafficLimit > 0 && c.bytesSent()+c.bytesReceived() > c.trafficLimit {
		return ErrLimitExceeded
	}
	if c.messageLimit > 0 && c.messagesSent()+c.messagesReceived() > c.trafficLimit {
		return ErrLimitExceeded
	}
	return nil
}

func (c *wireConduit) NextMessage() (SyncMessage, error) {
	msg, n, err := c.nextMessage()
	c.nBytesRecv.Add(int64(n))
	c.nMsgsRecv.Add(1)
	if err != nil {
		return nil, fmt.Errorf("receive message: %w", err)
	}
	if err = c.checkLimits(); err != nil {
		return nil, err
	}
	return msg, nil
}

func (c *wireConduit) nextMessage() (SyncMessage, int, error) {
	var b [1]byte
	if n, err := io.ReadFull(c.stream, b[:]); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, n, err
		}
		return nil, n, nil
	}
	mtype := MessageType(b[0])
	switch mtype {
	case MessageTypeDone:
		return &DoneMessage{}, 1, nil
	case MessageTypeEndRound:
		return &EndRoundMessage{}, 1, nil
	case MessageTypeItemBatch:
		return decodeMessage[ItemBatchMessage](c.stream)
	case MessageTypeEmptySet:
		return &EmptySetMessage{}, 1, nil
	case MessageTypeEmptyRange:
		return decodeMessage[EmptyRangeMessage](c.stream)
	case MessageTypeFingerprint:
		return decodeMessage[FingerprintMessage](c.stream)
	case MessageTypeRangeContents:
		return decodeMessage[RangeContentsMessage](c.stream)
	case MessageTypeProbe:
		return decodeMessage[ProbeMessage](c.stream)
	case MessageTypeSample:
		return decodeMessage[SampleMessage](c.stream)
	case MessageTypeRecent:
		return decodeMessage[RecentMessage](c.stream)
	default:
		return nil, 1, fmt.Errorf("invalid message code %02x", b[0])
	}
}

func (c *wireConduit) Send(m SyncMessage) error {
	select {
	case <-c.stopCh:
		return errors.New("conduit closed")
	case c.sendCh <- m:
		return nil
	}
}

func (c *wireConduit) bytesSent() int {
	return int(c.nBytesSent.Load())
}

func (c *wireConduit) bytesReceived() int {
	return int(c.nBytesRecv.Load())
}

func (c *wireConduit) messagesSent() int {
	return int(c.nMsgsSent.Load())
}

func (c *wireConduit) messagesReceived() int {
	return int(c.nMsgsRecv.Load())
}

func writeMessage(w io.Writer, m SyncMessage) (int, error) {
	b := []byte{byte(m.Type())}
	if n, err := w.Write(b); err != nil {
		return n, err
	}
	if enc, ok := m.(codec.Encodable); ok {
		n, err := codec.EncodeTo(w, enc)
		return n + 1, err
	}
	return 1, nil
}

func decodeMessage[T any, PT interface {
	SyncMessage
	codec.Decodable
	*T
}](r io.Reader) (SyncMessage, int, error) {
	v := PT(new(T))
	n, err := codec.DecodeFrom(r, v)
	if err != nil {
		return nil, n, err
	}
	return v, n, nil
}
