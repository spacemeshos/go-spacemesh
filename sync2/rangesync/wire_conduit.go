package rangesync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type sendable interface {
	codec.Encodable
	Type() MessageType
}

type wireConduit struct {
	stream     io.ReadWriter
	initReqBuf *bytes.Buffer
}

var _ Conduit = &wireConduit{}

func (c *wireConduit) NextMessage() (SyncMessage, error) {
	var b [1]byte
	if _, err := io.ReadFull(c.stream, b[:]); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, err
		}
		return nil, nil
	}
	mtype := MessageType(b[0])
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
		return &m, nil
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
	case MessageTypeProbe:
		var m ProbeMessage
		if _, err := codec.DecodeFrom(c.stream, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case MessageTypeSample:
		var m SampleMessage
		if _, err := codec.DecodeFrom(c.stream, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case MessageTypeRecent:
		var m RecentMessage
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

func (c *wireConduit) SendFingerprint(x, y types.Ordered, fp types.Fingerprint, count int) error {
	return c.send(&FingerprintMessage{
		RangeX:           OrderedToCompactHash(x),
		RangeY:           OrderedToCompactHash(y),
		RangeFingerprint: fp,
		NumItems:         uint32(count),
	})
}

func (c *wireConduit) SendEmptySet() error {
	return c.send(&EmptySetMessage{})
}

func (c *wireConduit) SendEmptyRange(x, y types.Ordered) error {
	return c.send(&EmptyRangeMessage{
		RangeX: OrderedToCompactHash(x),
		RangeY: OrderedToCompactHash(y),
	})
}

func (c *wireConduit) SendRangeContents(x, y types.Ordered, count int) error {
	return c.send(&RangeContentsMessage{
		RangeX:   OrderedToCompactHash(x),
		RangeY:   OrderedToCompactHash(y),
		NumItems: uint32(count),
	})
}

func (c *wireConduit) SendChunk(items []types.Ordered) error {
	msg := ItemBatchMessage{
		ContentKeys: KeyCollection{
			Keys: make([]types.KeyBytes, len(items)),
		},
	}
	for n, k := range items {
		msg.ContentKeys.Keys[n] = k.(types.KeyBytes)
	}
	return c.send(&msg)
}

func (c *wireConduit) SendEndRound() error {
	return c.send(&EndRoundMessage{})
}

func (c *wireConduit) SendDone() error {
	return c.send(&DoneMessage{})
}

func (c *wireConduit) SendProbe(x, y types.Ordered, fp types.Fingerprint, sampleSize int) error {
	m := &ProbeMessage{
		RangeFingerprint: fp,
		SampleSize:       uint32(sampleSize),
	}
	if x == nil && y == nil {
		return c.send(m)
	} else if x == nil || y == nil {
		panic("BUG: SendProbe: bad range: just one of the bounds is nil")
	}
	m.RangeX = OrderedToCompactHash(x)
	m.RangeY = OrderedToCompactHash(y)
	return c.send(m)
}

func (c *wireConduit) SendSample(
	x, y types.Ordered,
	fp types.Fingerprint,
	count, sampleSize int,
	seq types.Seq,
) error {
	m := &SampleMessage{
		RangeFingerprint: fp,
		NumItems:         uint32(count),
		Sample:           make([]MinhashSampleItem, sampleSize),
	}
	n := 0
	for k, err := range seq {
		if err != nil {
			return err
		}
		m.Sample[n] = MinhashSampleItemFromKeyBytes(k.(types.KeyBytes))
		n++
		if n == sampleSize {
			break
		}
	}
	if x == nil && y == nil {
		return c.send(m)
	} else if x == nil || y == nil {
		panic("BUG: SendProbe: bad range: just one of the bounds is nil")
	}
	m.RangeX = OrderedToCompactHash(x)
	m.RangeY = OrderedToCompactHash(y)
	return c.send(m)
}

func (c *wireConduit) SendRecent(since time.Time) error {
	return c.send(&RecentMessage{
		SinceTime: uint64(since.UnixNano()),
	})
}

func (c *wireConduit) withInitialRequest(toCall func(Conduit) error) ([]byte, error) {
	c.initReqBuf = new(bytes.Buffer)
	defer func() { c.initReqBuf = nil }()
	if err := toCall(c); err != nil {
		return nil, err
	}
	return c.initReqBuf.Bytes(), nil
}

func (c *wireConduit) handleStream(ctx context.Context, stream io.ReadWriter, rsr *RangeSetReconciler) error {
	c.stream = stream
	for {
		// Process() will receive all items and messages from the peer
		syncDone, err := rsr.Process(ctx, c)
		if err != nil {
			return err
		} else if syncDone {
			return nil
		}
	}
}

func (c *wireConduit) ShortenKey(k types.Ordered) types.Ordered {
	return MinhashSampleItemFromKeyBytes(k.(types.KeyBytes))
}
