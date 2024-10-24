package rangesync

import (
	"context"
	"fmt"
	"io"

	"github.com/spacemeshos/go-spacemesh/p2p"
)

type PairwiseSetSyncer struct {
	r           Requester
	name        string
	opts        []RangeSetReconcilerOption
	conduitOpts []ConduitOption
	sent        int
	recv        int
}

func NewPairwiseSetSyncer(
	r Requester,
	name string,
	opts []RangeSetReconcilerOption,
	conduitOpts []ConduitOption,
) *PairwiseSetSyncer {
	return &PairwiseSetSyncer{
		r:           r,
		name:        name,
		opts:        opts,
		conduitOpts: conduitOpts,
	}
}

func (pss *PairwiseSetSyncer) updateCounts(c *wireConduit) {
	pss.sent += c.bytesSent()
	pss.recv += c.bytesReceived()
}

func (pss *PairwiseSetSyncer) Probe(
	ctx context.Context,
	peer p2p.Peer,
	os OrderedSet,
	x, y KeyBytes,
) (ProbeResult, error) {
	var pr ProbeResult
	rsr := NewRangeSetReconciler(os, pss.opts...)
	initReq := []byte(pss.name)
	if err := pss.r.StreamRequest(
		ctx, peer, initReq,
		func(ctx context.Context, stream io.ReadWriter) (err error) {
			c := startWireConduit(ctx, stream, pss.conduitOpts...)
			defer func() {
				// If the conduit is not closed by this point, stop it
				// interrupting any ongoing send operations
				c.Stop()
				pss.updateCounts(c)
			}()
			info, err := rsr.InitiateProbe(c, x, y)
			if err != nil {
				return fmt.Errorf("initiating probe: %w", err)
			}
			pr, err = rsr.HandleProbeResponse(c, info)
			if err != nil {
				return fmt.Errorf("handling probe response: %w", err)
			}
			// Wait for the messages to be sent before closing the conduit
			c.End()
			return nil
		}); err != nil {
		return ProbeResult{}, err
	}
	return pr, nil
}

func (pss *PairwiseSetSyncer) requestCallback(
	ctx context.Context,
	stream io.ReadWriter,
	rsr *RangeSetReconciler,
	x, y KeyBytes,
) error {
	c := startWireConduit(ctx, stream, pss.conduitOpts...)
	defer func() {
		c.Stop()
		pss.updateCounts(c)
	}()
	if err := rsr.Initiate(c, x, y); err != nil {
		return fmt.Errorf("initiating sync: %w", err)
	}
	if err := rsr.Run(c); err != nil {
		return fmt.Errorf("running sync: %w", err)
	}
	c.End()
	return nil
}

func (pss *PairwiseSetSyncer) Sync(
	ctx context.Context,
	peer p2p.Peer,
	os OrderedSet,
	x, y KeyBytes,
) error {
	rsr := NewRangeSetReconciler(os, pss.opts...)
	initReq := []byte(pss.name)
	return pss.r.StreamRequest(
		ctx, peer, initReq,
		func(ctx context.Context, stream io.ReadWriter) (err error) {
			return pss.requestCallback(ctx, stream, rsr, x, y)
		})
}

func (pss *PairwiseSetSyncer) Serve(ctx context.Context, stream io.ReadWriter, os OrderedSet) error {
	c := startWireConduit(ctx, stream, pss.conduitOpts...)
	defer c.Stop()
	rsr := NewRangeSetReconciler(os, pss.opts...)
	if err := rsr.Run(c); err != nil {
		return err
	}
	c.End()
	return nil
}

func (pss *PairwiseSetSyncer) Register(d *Dispatcher, os OrderedSet) {
	d.Register(pss.name, func(ctx context.Context, s io.ReadWriter) error {
		return pss.Serve(ctx, s, os)
	})
}

func (pss *PairwiseSetSyncer) Sent() int {
	return pss.sent
}

func (pss *PairwiseSetSyncer) Received() int {
	return pss.recv
}
