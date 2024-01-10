package hashsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

type outboundMessage struct {
	code MessageType // TODO: "mt"
	msg  codec.Encodable
}

type conduitState int

type wireConduit struct {
	i           server.Interactor
	pendingMsgs []SyncMessage
	initReqBuf  *bytes.Buffer
	// rmmePrint   bool
}

var _ Conduit = &wireConduit{}

func (c *wireConduit) reset() {
	c.pendingMsgs = nil
}

// receive receives a single frame from the Interactor and decodes one
// or more SyncMessages from it. The frames contain just one message
// except for the initial frame which may contain multiple messages
// b/c of the way Server handles the initial request
func (c *wireConduit) receive() (msgs []SyncMessage, err error) {
	data, err := c.i.Receive()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("zero length sync message")
	}
	b := bytes.NewBuffer(data)
	for {
		code, err := b.ReadByte()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				// this shouldn't really happen
				return nil, err
			}
			// fmt.Fprintf(os.Stderr, "QQQQQ: wireConduit: decoded msgs: %#v\n", msgs)
			return msgs, nil
		}
		mtype := MessageType(code)
		// fmt.Fprintf(os.Stderr, "QQQQQ: wireConduit: receive message type %s\n", mtype)
		switch mtype {
		case MessageTypeDone:
			msgs = append(msgs, &DoneMessage{})
		case MessageTypeEndRound:
			msgs = append(msgs, &EndRoundMessage{})
		case MessageTypeItemBatch:
			var m ItemBatchMessage
			if _, err := codec.DecodeFrom(b, &m); err != nil {
				return nil, err
			}
			msgs = append(msgs, &m)
		case MessageTypeEmptySet:
			msgs = append(msgs, &EmptySetMessage{})
		case MessageTypeEmptyRange:
			var m EmptyRangeMessage
			if _, err := codec.DecodeFrom(b, &m); err != nil {
				return nil, err
			}
			msgs = append(msgs, &m)
		case MessageTypeFingerprint:
			var m FingerprintMessage
			if _, err := codec.DecodeFrom(b, &m); err != nil {
				return nil, err
			}
			msgs = append(msgs, &m)
		case MessageTypeRangeContents:
			var m RangeContentsMessage
			if _, err := codec.DecodeFrom(b, &m); err != nil {
				return nil, err
			}
			msgs = append(msgs, &m)
		default:
			return nil, fmt.Errorf("invalid message code %02x", code)
		}
	}
}

func (c *wireConduit) send(m SyncMessage) error {
	// fmt.Fprintf(os.Stderr, "QQQQQ: wireConduit: sending %s m %#v\n", m.Type(), m)
	msg := []byte{byte(m.Type())}
	// if c.rmmePrint {
	// 	fmt.Fprintf(os.Stderr, "QQQQQ: send: %s\n", SyncMessageToString(m))
	// }
	encoded, err := codec.Encode(m.(codec.Encodable))
	if err != nil {
		return fmt.Errorf("error encoding %T: %w", m, err)
	}
	msg = append(msg, encoded...)
	if c.initReqBuf != nil {
		c.initReqBuf.Write(msg)
	} else {
		if err := c.i.Send(msg); err != nil {
			return err
		}
	}
	return nil
}

// NextMessage implements Conduit.
func (c *wireConduit) NextMessage() (SyncMessage, error) {
	if len(c.pendingMsgs) != 0 {
		m := c.pendingMsgs[0]
		c.pendingMsgs = c.pendingMsgs[1:]
		// if c.rmmePrint {
		// 	fmt.Fprintf(os.Stderr, "QQQQQ: recv: %s\n", SyncMessageToString(m))
		// }
		return m, nil
	}

	msgs, err := c.receive()
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, nil
	}

	c.pendingMsgs = msgs[1:]
	// if c.rmmePrint {
	// 	fmt.Fprintf(os.Stderr, "QQQQQ: recv: %s\n", SyncMessageToString(msgs[0]))
	// }
	return msgs[0], nil
}

func (c *wireConduit) SendFingerprint(x Ordered, y Ordered, fingerprint any, count int) error {
	return c.send(&FingerprintMessage{
		RangeX:           x.(types.Hash32),
		RangeY:           y.(types.Hash32),
		RangeFingerprint: fingerprint.(types.Hash12),
		NumItems:         uint32(count),
	})
}

func (c *wireConduit) SendEmptySet() error {
	return c.send(&EmptySetMessage{})
}

func (c *wireConduit) SendEmptyRange(x Ordered, y Ordered) error {
	return c.send(&EmptyRangeMessage{RangeX: x.(types.Hash32), RangeY: y.(types.Hash32)})
}

func (c *wireConduit) SendRangeContents(x Ordered, y Ordered, count int) error {
	return c.send(&RangeContentsMessage{
		RangeX:   x.(types.Hash32),
		RangeY:   y.(types.Hash32),
		NumItems: uint32(count),
	})
}

func (c *wireConduit) SendItems(count, itemChunkSize int, it Iterator) error {
	for i := 0; i < count; i += itemChunkSize {
		var msg ItemBatchMessage
		n := min(itemChunkSize, count-i)
		for n > 0 {
			if it.Key() == nil {
				panic("fakeConduit.SendItems: went got to the end of the tree")
			}
			msg.Contents = append(msg.Contents, it.Key().(types.Hash32))
			it.Next()
			n--
		}
		if err := c.send(&msg); err != nil {
			return err
		}
	}
	return nil
}

func (c *wireConduit) SendEndRound() error {
	return c.send(&EndRoundMessage{})
}

func (c *wireConduit) SendDone() error {
	return c.send(&DoneMessage{})
}

func (c *wireConduit) withInitialRequest(toCall func(Conduit) error) ([]byte, error) {
	c.initReqBuf = new(bytes.Buffer)
	defer func() { c.initReqBuf = nil }()
	if err := toCall(c); err != nil {
		return nil, err
	}
	return c.initReqBuf.Bytes(), nil
}

func makeHandler(rsr *RangeSetReconciler, c *wireConduit, done chan struct{}) server.InteractiveHandler {
	return func(ctx context.Context, i server.Interactor) (time.Duration, error) {
		defer func() {
			if done != nil {
				close(done)
			}
		}()
		c.i = i
		for {
			c.reset()
			// Process() will receive all items and messages from the peer
			syncDone, err := rsr.Process(c)
			if err != nil {
				// do not close done if we're returning an
				// error, as the channel will be closed in the
				// error handler func
				done = nil
				return 0, err
			} else if syncDone {
				return 0, nil
			}
		}
	}
}

func MakeServerHandler(rsr *RangeSetReconciler) server.InteractiveHandler {
	return func(ctx context.Context, i server.Interactor) (time.Duration, error) {
		var c wireConduit
		h := makeHandler(rsr, &c, nil)
		return h(ctx, i)
	}
}

func SyncStore(ctx context.Context, r requester, peer p2p.Peer, rsr *RangeSetReconciler) error {
	var c wireConduit
	// c.rmmePrint = true
	initReq, err := c.withInitialRequest(rsr.Initiate)
	if err != nil {
		return err
	}
	done := make(chan struct{}, 1)
	h := makeHandler(rsr, &c, done)
	var reqErr error
	if err = r.InteractiveRequest(ctx, peer, initReq, h, func(err error) {
		reqErr = err
		close(done)
	}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return reqErr
	}
}

// TODO: HashSyncer object (SyncStore, also server handler, implementing ServerHandler)
// TODO: HashSyncer options instead of itemChunkSize (WithItemChunkSize, WithMaxSendRange)
// TODO: duration
// TODO: validate counts
// TODO: don't forget about Initiate!!!
// TBD: use MessageType instead of byte
