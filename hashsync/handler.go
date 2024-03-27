package hashsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

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
	stream     io.ReadWriter
	initReqBuf *bytes.Buffer
	newValue   NewValueFunc
	// rmmePrint   bool
}

var _ Conduit = &wireConduit{}

// NextMessage implements Conduit.
func (c *wireConduit) NextMessage() (SyncMessage, error) {
	var b [1]byte
	if _, err := io.ReadFull(c.stream, b[:]); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, err
		}
		return nil, nil
	}
	mtype := MessageType(b[0])
	// fmt.Fprintf(os.Stderr, "QQQQQ: wireConduit: receive message type %s\n", mtype)
	switch mtype {
	case MessageTypeDone:
		return &DoneMessage{}, nil
	case MessageTypeEndRound:
		return &EndRoundMessage{}, nil
	case MessageTypeItemBatch:
		var m ItemBatchMessage
		if _, err := codec.DecodeFrom(c.stream, &m); err != nil {
			return nil, err
		}
		dm, err := decodeItemBatchMessage(&m, c.newValue)
		if err != nil {
			return nil, err
		}
		return dm, nil
	case MessageTypeEmptySet:
		return &EmptySetMessage{}, nil
	case MessageTypeEmptyRange:
		var m EmptyRangeMessage
		if _, err := codec.DecodeFrom(c.stream, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case MessageTypeFingerprint:
		var m FingerprintMessage
		if _, err := codec.DecodeFrom(c.stream, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case MessageTypeRangeContents:
		var m RangeContentsMessage
		if _, err := codec.DecodeFrom(c.stream, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case MessageTypeQuery:
		var m QueryMessage
		if _, err := codec.DecodeFrom(c.stream, &m); err != nil {
			return nil, err
		}
		return &m, nil
	default:
		return nil, fmt.Errorf("invalid message code %02x", b[0])
	}
}

func (c *wireConduit) send(m sendable) error {
	var stream io.Writer
	if c.initReqBuf != nil {
		stream = c.initReqBuf
	} else if c.stream == nil {
		panic("BUG: wireConduit: no stream")
	} else {
		stream = c.stream
	}
	b := []byte{byte(m.Type())}
	if _, err := stream.Write(b); err != nil {
		return err
	}
	_, err := codec.EncodeTo(stream, m)
	return err
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

func (c *wireConduit) handleStream(stream io.ReadWriter, rsr *RangeSetReconciler) error {
	c.stream = stream
	for {
		// Process() will receive all items and messages from the peer
		syncDone, err := rsr.Process(c)
		if err != nil {
			return err
		} else if syncDone {
			return nil
		}
	}
}

func MakeServerHandler(is ItemStore, opts ...Option) server.StreamHandler {
	return func(ctx context.Context, req []byte, stream io.ReadWriter) error {
		c := wireConduit{newValue: is.New}
		rsr := NewRangeSetReconciler(is, opts...)
		s := struct {
			io.Reader
			io.Writer
		}{
			// prepend the received request to data being read
			Reader: io.MultiReader(bytes.NewBuffer(req), stream),
			Writer: stream,
		}
		return c.handleStream(s, rsr)
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
	return r.StreamRequest(ctx, peer, initReq, func(ctx context.Context, stream io.ReadWriter) error {
		return c.handleStream(stream, rsr)
	})
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
	err = r.StreamRequest(ctx, peer, initReq, func(ctx context.Context, stream io.ReadWriter) error {
		c.stream = stream
		var err error
		fp, count, err = rsr.HandleProbeResponse(&c)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return fp, count, nil
}

// TODO: request duration
// TODO: validate counts
// TODO: don't forget about Initiate!!!
// TBD: use MessageType instead of byte
