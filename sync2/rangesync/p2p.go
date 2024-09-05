package rangesync

import (
	"bytes"
	"context"
	"io"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type PairwiseSetSyncer struct {
	r    Requester
	opts []RangeSetReconcilerOption
}

func NewPairwiseSetSyncer(r Requester, opts []RangeSetReconcilerOption) *PairwiseSetSyncer {
	return &PairwiseSetSyncer{r: r, opts: opts}
}

func (pss *PairwiseSetSyncer) Probe(
	ctx context.Context,
	peer p2p.Peer,
	os OrderedSet,
	x, y types.KeyBytes,
) (ProbeResult, error) {
	var (
		err     error
		initReq []byte
		info    RangeInfo
		pr      ProbeResult
	)
	var c wireConduit
	rsr := NewRangeSetReconciler(os, pss.opts...)
	if x == nil {
		initReq, err = c.withInitialRequest(func(c Conduit) error {
			info, err = rsr.InitiateProbe(ctx, c)
			return err
		})
	} else {
		initReq, err = c.withInitialRequest(func(c Conduit) error {
			info, err = rsr.InitiateBoundedProbe(ctx, c, x, y)
			return err
		})
	}
	if err != nil {
		return ProbeResult{}, err
	}
	err = pss.r.StreamRequest(ctx, peer, initReq, func(ctx context.Context, stream io.ReadWriter) error {
		var err error
		c.begin(ctx, stream)
		defer c.end()
		pr, err = rsr.HandleProbeResponse(&c, info)
		return err
	})
	if err != nil {
		return ProbeResult{}, err
	}
	return pr, nil
}

func (pss *PairwiseSetSyncer) Sync(
	ctx context.Context,
	peer p2p.Peer,
	os OrderedSet,
	x, y types.KeyBytes,
) error {
	var c wireConduit
	rsr := NewRangeSetReconciler(os, pss.opts...)
	var (
		initReq []byte
		err     error
	)
	if x == nil {
		initReq, err = c.withInitialRequest(func(c Conduit) error {
			return rsr.Initiate(ctx, c)
		})
	} else {
		initReq, err = c.withInitialRequest(func(c Conduit) error {
			return rsr.InitiateBounded(ctx, c, x, y)
		})
	}
	if err != nil {
		return err
	}
	return pss.r.StreamRequest(ctx, peer, initReq, func(ctx context.Context, stream io.ReadWriter) error {
		c.begin(ctx, stream)
		defer c.end()
		if err := rsr.Run(ctx, &c); err != nil {
			c.closeStream() // stop the writer
			return err
		}
		return nil
	})
}

func (pss *PairwiseSetSyncer) Serve(
	ctx context.Context,
	req []byte,
	stream io.ReadWriter,
	os OrderedSet,
) error {
	var c wireConduit
	rsr := NewRangeSetReconciler(os, pss.opts...)
	s := struct {
		io.Reader
		io.Writer
	}{
		// prepend the received request to data being read
		Reader: io.MultiReader(bytes.NewBuffer(req), stream),
		Writer: stream,
	}
	c.begin(ctx, s)
	defer c.end()
	if err := rsr.Run(ctx, &c); err != nil {
		c.closeStream() // stop the writer
		return err
	}
	return nil
}
