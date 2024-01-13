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

type sendable interface {
	codec.Encodable
	Type() MessageType
}

type decodedItemBatchMessage struct {
	ContentKeys   []types.Hash32
	ContentValues []any
}

var _ SyncMessage = &decodedItemBatchMessage{}

func (m *decodedItemBatchMessage) Type() MessageType { return MessageTypeItemBatch }
func (m *decodedItemBatchMessage) X() Ordered        { return nil }
func (m *decodedItemBatchMessage) Y() Ordered        { return nil }
func (m *decodedItemBatchMessage) Fingerprint() any  { return nil }
func (m *decodedItemBatchMessage) Count() int        { return 0 }
func (m *decodedItemBatchMessage) Keys() []Ordered {
	r := make([]Ordered, len(m.ContentKeys))
	for n, k := range m.ContentKeys {
		r[n] = k
	}
	return r
}
func (m *decodedItemBatchMessage) Values() []any {
	r := make([]any, len(m.ContentValues))
	for n, v := range m.ContentValues {
		r[n] = v
	}
	return r
}

func (m *decodedItemBatchMessage) encode() (*ItemBatchMessage, error) {
	var b bytes.Buffer
	for _, v := range m.ContentValues {
		_, err := codec.EncodeTo(&b, v.(codec.Encodable))
		if err != nil {
			return nil, err
		}
	}
	return &ItemBatchMessage{
		ContentKeys:   m.ContentKeys,
		ContentValues: b.Bytes(),
	}, nil
}

func decodeItemBatchMessage(m *ItemBatchMessage, newValue NewValueFunc) (*decodedItemBatchMessage, error) {
	d := &decodedItemBatchMessage{ContentKeys: m.ContentKeys}
	b := bytes.NewBuffer(m.ContentValues)
	for b.Len() != 0 {
		v := newValue().(codec.Decodable)
		if _, err := codec.DecodeFrom(b, v); err != nil {
			return nil, err
		}
		d.ContentValues = append(d.ContentValues, v)
	}
	if len(d.ContentValues) != len(d.ContentKeys) {
		return nil, fmt.Errorf("mismatched key / value counts: %d / %d",
			len(d.ContentKeys), len(d.ContentValues))
	}
	return d, nil
}

type conduitState int

type wireConduit struct {
	i           server.Interactor
	pendingMsgs []SyncMessage
	initReqBuf  *bytes.Buffer
	newValue    NewValueFunc
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
			dm, err := decodeItemBatchMessage(&m, c.newValue)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, dm)
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
		case MessageTypeQuery:
			var m QueryMessage
			if _, err := codec.DecodeFrom(b, &m); err != nil {
				return nil, err
			}
			msgs = append(msgs, &m)
		default:
			return nil, fmt.Errorf("invalid message code %02x", code)
		}
	}
}

func (c *wireConduit) send(m sendable) error {
	// fmt.Fprintf(os.Stderr, "QQQQQ: wireConduit: sending %s m %#v\n", m.Type(), m)
	msg := []byte{byte(m.Type())}
	// if c.rmmePrint {
	// 	fmt.Fprintf(os.Stderr, "QQQQQ: send: %s\n", SyncMessageToString(m))
	// }
	encoded, err := codec.Encode(m)
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

func (c *wireConduit) SendFingerprint(x, y Ordered, fingerprint any, count int) error {
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

func (c *wireConduit) SendEmptyRange(x, y Ordered) error {
	return c.send(&EmptyRangeMessage{RangeX: x.(types.Hash32), RangeY: y.(types.Hash32)})
}

func (c *wireConduit) SendRangeContents(x, y Ordered, count int) error {
	return c.send(&RangeContentsMessage{
		RangeX:   x.(types.Hash32),
		RangeY:   y.(types.Hash32),
		NumItems: uint32(count),
	})
}

func (c *wireConduit) SendItems(count, itemChunkSize int, it Iterator) error {
	for i := 0; i < count; i += itemChunkSize {
		var msg decodedItemBatchMessage
		n := min(itemChunkSize, count-i)
		for n > 0 {
			if it.Key() == nil {
				panic("fakeConduit.SendItems: went got to the end of the tree")
			}
			msg.ContentKeys = append(msg.ContentKeys, it.Key().(types.Hash32))
			msg.ContentValues = append(msg.ContentValues, it.Value())
			it.Next()
			n--
		}
		encoded, err := msg.encode()
		if err != nil {
			return err
		}
		if err := c.send(encoded); err != nil {
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

func (c *wireConduit) SendQuery(x, y Ordered) error {
	if x == nil && y == nil {
		return c.send(&QueryMessage{})
	} else if x == nil || y == nil {
		panic("BUG: SendQuery: bad range: just one of the bounds is nil")
	}
	xh := x.(types.Hash32)
	yh := y.(types.Hash32)
	return c.send(&QueryMessage{RangeX: &xh, RangeY: &yh})
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

func MakeServerHandler(is ItemStore, opts ...Option) server.InteractiveHandler {
	return func(ctx context.Context, i server.Interactor) (time.Duration, error) {
		c := wireConduit{newValue: is.New}
		rsr := NewRangeSetReconciler(is, opts...)
		h := makeHandler(rsr, &c, nil)
		return h(ctx, i)
	}
}

func BoundedSyncStore(ctx context.Context, r requester, peer p2p.Peer, is ItemStore, x, y types.Hash32, opts ...Option) error {
	return syncStore(ctx, r, peer, is, &x, &y, opts)
}

func SyncStore(ctx context.Context, r requester, peer p2p.Peer, is ItemStore, opts ...Option) error {
	return syncStore(ctx, r, peer, is, nil, nil, opts)
}

func syncStore(ctx context.Context, r requester, peer p2p.Peer, is ItemStore, x, y *types.Hash32, opts []Option) error {
	c := wireConduit{newValue: is.New}
	rsr := NewRangeSetReconciler(is, opts...)
	// c.rmmePrint = true
	var (
		initReq []byte
		err     error
	)
	if x == nil {
		initReq, err = c.withInitialRequest(rsr.Initiate)
	} else {
		initReq, err = c.withInitialRequest(func(c Conduit) error {
			return rsr.InitiateBounded(c, *x, *y)
		})
	}
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
		<-done
		return ctx.Err()
	case <-done:
		return reqErr
	}
}

func Probe(ctx context.Context, r requester, peer p2p.Peer, opts ...Option) (fp any, count int, err error) {
	return boundedProbe(ctx, r, peer, nil, nil, opts)
}

func BoundedProbe(ctx context.Context, r requester, peer p2p.Peer, x, y types.Hash32, opts ...Option) (fp any, count int, err error) {
	return boundedProbe(ctx, r, peer, &x, &y, opts)
}

func boundedProbe(ctx context.Context, r requester, peer p2p.Peer, x, y *types.Hash32, opts []Option) (fp any, count int, err error) {
	c := wireConduit{
		newValue: func() any { return nil }, // not used
	}
	rsr := NewRangeSetReconciler(nil, opts...)
	// c.rmmePrint = true
	var initReq []byte
	if x == nil {
		initReq, err = c.withInitialRequest(func(c Conduit) error {
			return rsr.InitiateProbe(c)
		})
	} else {
		initReq, err = c.withInitialRequest(func(c Conduit) error {
			return rsr.InitiateBoundedProbe(c, *x, *y)
		})
	}
	if err != nil {
		return nil, 0, err
	}
	done := make(chan struct{}, 2)
	h := func(ctx context.Context, i server.Interactor) (time.Duration, error) {
		defer func() {
			done <- struct{}{}
		}()
		c.i = i
		var err error
		fp, count, err = rsr.HandleProbeResponse(&c)
		return 0, err
	}
	var reqErr error
	if err = r.InteractiveRequest(ctx, peer, initReq, h, func(err error) {
		reqErr = err
		done <- struct{}{}
	}); err != nil {
		return nil, 0, err
	}
	select {
	case <-ctx.Done():
		<-done
		return nil, 0, ctx.Err()
	case <-done:
		if reqErr != nil {
			return nil, 0, reqErr
		}
		return fp, count, nil
	}
}

// TODO: request duration
// TODO: validate counts
// TODO: don't forget about Initiate!!!
// TBD: use MessageType instead of byte
