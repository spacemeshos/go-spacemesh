package hashsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

type sendable interface {
	codec.Encodable
	Type() MessageType
}

// QQQQQ: rmme
var (
	numRead    atomic.Int64
	numWritten atomic.Int64
)

type rmmeCountingStream struct {
	io.ReadWriter
}

// Read implements io.ReadWriter.
func (r *rmmeCountingStream) Read(p []byte) (n int, err error) {
	n, err = r.ReadWriter.Read(p)
	numRead.Add(int64(n))
	return n, err
}

// Write implements io.ReadWriter.
func (r *rmmeCountingStream) Write(p []byte) (n int, err error) {
	n, err = r.ReadWriter.Write(p)
	numWritten.Add(int64(n))
	return n, err
}

type conduitState int

type wireConduit struct {
	stream     io.ReadWriter
	initReqBuf *bytes.Buffer
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
	case MessageTypeProbeResponse:
		var m ProbeResponseMessage
		if _, err := codec.DecodeFrom(c.stream, &m); err != nil {
			return nil, err
		}
		return &m, nil
	default:
		return nil, fmt.Errorf("invalid message code %02x", b[0])
	}
}

func (c *wireConduit) send(m sendable) error {
	// fmt.Fprintf(os.Stderr, "QQQQQ: send: %s: %#v\n", m.Type(), m)
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
		// TBD: do not use chunks, just stream the contentkeys
		var msg ItemBatchMessage
		n := min(itemChunkSize, count-i)
		for n > 0 {
			k, err := it.Key()
			if err != nil {
				return err
			}
			msg.ContentKeys = append(msg.ContentKeys, k.(types.Hash32))
			if err := it.Next(); err != nil {
				return err
			}
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

func (c *wireConduit) SendProbe(x, y Ordered, fingerprint any, sampleSize int) error {
	m := &ProbeMessage{
		RangeFingerprint: fingerprint.(types.Hash12),
		SampleSize:       uint32(sampleSize),
	}
	if x == nil && y == nil {
		return c.send(m)
	} else if x == nil || y == nil {
		panic("BUG: SendProbe: bad range: just one of the bounds is nil")
	}
	xh := x.(types.Hash32)
	yh := y.(types.Hash32)
	m.RangeX = &xh
	m.RangeY = &yh
	return c.send(m)
}

func (c *wireConduit) SendProbeResponse(x, y Ordered, fingerprint any, count, sampleSize int, it Iterator) error {
	m := &ProbeResponseMessage{
		RangeFingerprint: fingerprint.(types.Hash12),
		NumItems:         uint32(count),
		Sample:           make([]MinhashSampleItem, sampleSize),
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: begin sending items\n")
	for n := 0; n < sampleSize; n++ {
		k, err := it.Key()
		if err != nil {
			return err
		}
		m.Sample[n] = MinhashSampleItemFromHash32(k.(types.Hash32))
		// fmt.Fprintf(os.Stderr, "QQQQQ: m.Sample[%d] = %s\n", n, m.Sample[n])
		if err := it.Next(); err != nil {
			return err
		}
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: end sending items\n")
	if x == nil && y == nil {
		return c.send(m)
	} else if x == nil || y == nil {
		panic("BUG: SendProbe: bad range: just one of the bounds is nil")
	}
	xh := x.(types.Hash32)
	yh := y.(types.Hash32)
	m.RangeX = &xh
	m.RangeY = &yh
	return c.send(m)
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

// ShortenKey implements Conduit.
func (c *wireConduit) ShortenKey(k Ordered) Ordered {
	return MinhashSampleItemFromHash32(k.(types.Hash32))
}

type PairwiseStoreSyncer struct {
	r    Requester
	opts []RangeSetReconcilerOption
}

var _ PairwiseSyncer = &PairwiseStoreSyncer{}

func NewPairwiseStoreSyncer(r Requester, opts []RangeSetReconcilerOption) *PairwiseStoreSyncer {
	return &PairwiseStoreSyncer{r: r, opts: opts}
}

func (pss *PairwiseStoreSyncer) Probe(
	ctx context.Context,
	peer p2p.Peer,
	is ItemStore,
	x, y *types.Hash32,
) (ProbeResult, error) {
	var (
		err     error
		initReq []byte
		info    RangeInfo
		pr      ProbeResult
	)
	var c wireConduit
	rsr := NewRangeSetReconciler(is, pss.opts...)
	if x == nil {
		initReq, err = c.withInitialRequest(func(c Conduit) error {
			info, err = rsr.InitiateProbe(c)
			return err
		})
	} else {
		initReq, err = c.withInitialRequest(func(c Conduit) error {
			info, err = rsr.InitiateBoundedProbe(c, *x, *y)
			return err
		})
	}
	if err != nil {
		return ProbeResult{}, err
	}
	err = pss.r.StreamRequest(ctx, peer, initReq, func(ctx context.Context, stream io.ReadWriter) error {
		c.stream = stream
		var err error
		pr, err = rsr.HandleProbeResponse(&c, info)
		return err
	})
	if err != nil {
		return ProbeResult{}, err
	}
	return pr, nil
}

func (pss *PairwiseStoreSyncer) SyncStore(
	ctx context.Context,
	peer p2p.Peer,
	is ItemStore,
	x, y *types.Hash32,
) error {
	var c wireConduit
	rsr := NewRangeSetReconciler(is, pss.opts...)
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
	return pss.r.StreamRequest(ctx, peer, initReq, func(ctx context.Context, stream io.ReadWriter) error {
		s := &rmmeCountingStream{ReadWriter: stream}
		return c.handleStream(ctx, s, rsr)
	})
}

func (pss *PairwiseStoreSyncer) Serve(
	ctx context.Context,
	req []byte,
	stream io.ReadWriter,
	is ItemStore,
) error {
	var c wireConduit
	rsr := NewRangeSetReconciler(is, pss.opts...)
	s := struct {
		io.Reader
		io.Writer
	}{
		// prepend the received request to data being read
		Reader: io.MultiReader(bytes.NewBuffer(req), stream),
		Writer: stream,
	}
	return c.handleStream(ctx, s, rsr)
}

// TODO: request duration
// TODO: validate counts
// TODO: don't forget about Initiate!!!
// TBD: use MessageType instead of byte
