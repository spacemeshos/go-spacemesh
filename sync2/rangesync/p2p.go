package rangesync

import (
	"context"
	"fmt"
	"io"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
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
	x, y types.KeyBytes,
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
				c.stop()
				pss.updateCounts(c)
			}()
			info, err := rsr.InitiateProbe(ctx, c, x, y)
			if err != nil {
				return fmt.Errorf("error initiating probe: %w", err)
			}
			pr, err = rsr.HandleProbeResponse(c, info)
			if err != nil {
				return fmt.Errorf("error handling probe response: %w", err)
			}
			// Wait for the messages to be sent before closing the conduit
			c.end()
			return nil
		}); err != nil {
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
	rsr := NewRangeSetReconciler(os, pss.opts...)
	initReq := []byte(pss.name)
	return pss.r.StreamRequest(
		ctx, peer, initReq,
		func(ctx context.Context, stream io.ReadWriter) (err error) {
			c := startWireConduit(ctx, stream, pss.conduitOpts...)
			defer func() {
				c.stop()
				pss.updateCounts(c)
			}()
			if err := rsr.Initiate(ctx, c, x, y); err != nil {
				return fmt.Errorf("error initiating sync: %w", err)
			}
			if err := rsr.Run(ctx, c); err != nil {
				return fmt.Errorf("error running sync: %w", err)
			}
			c.end()
			return nil
		})
}

func (pss *PairwiseSetSyncer) Serve(ctx context.Context, stream io.ReadWriter, os OrderedSet) error {
	c := startWireConduit(ctx, stream, pss.conduitOpts...)
	defer c.stop()
	rsr := NewRangeSetReconciler(os, pss.opts...)
	if err := rsr.Run(ctx, c); err != nil {
		return err
	}
	c.end()
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
