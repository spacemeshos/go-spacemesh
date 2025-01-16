package rangesync

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/p2p"
)

type PairwiseSetSyncer struct {
	logger *zap.Logger
	r      Requester
	name   string
	cfg    RangeSetReconcilerConfig
	sent   atomic.Int64
	recv   atomic.Int64
	tracer Tracer
	clock  clockwork.Clock
}

func NewPairwiseSetSyncerInternal(
	logger *zap.Logger,
	r Requester,
	name string,
	cfg RangeSetReconcilerConfig,
	tracer Tracer,
	clock clockwork.Clock,
) *PairwiseSetSyncer {
	return &PairwiseSetSyncer{
		logger: logger,
		r:      r,
		name:   name,
		cfg:    cfg,
		tracer: tracer,
		clock:  clock,
	}
}

func NewPairwiseSetSyncer(
	logger *zap.Logger,
	r Requester,
	name string,
	cfg RangeSetReconcilerConfig,
) *PairwiseSetSyncer {
	return NewPairwiseSetSyncerInternal(logger, r, name, cfg, nullTracer{}, clockwork.NewRealClock())
}

func (pss *PairwiseSetSyncer) updateCounts(c *wireConduit) {
	pss.sent.Add(int64(c.bytesSent()))
	pss.recv.Add(int64(c.bytesReceived()))
}

func (pss *PairwiseSetSyncer) createReconciler(os OrderedSet) *RangeSetReconciler {
	return NewRangeSetReconcilerInternal(pss.logger, pss.cfg, os, pss.tracer, pss.clock)
}

func (pss *PairwiseSetSyncer) Probe(
	ctx context.Context,
	peer p2p.Peer,
	os OrderedSet,
	x, y KeyBytes,
) (pr ProbeResult, err error) {
	rsr := pss.createReconciler(os)
	initReq := []byte(pss.name)
	if err = pss.r.StreamRequest(
		ctx, peer, initReq,
		func(ctx context.Context, stream io.ReadWriter) (err error) {
			c := startWireConduit(ctx, stream, pss.cfg)
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
	c := startWireConduit(ctx, stream, pss.cfg)
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
	rsr := pss.createReconciler(os)
	initReq := []byte(pss.name)
	return pss.r.StreamRequest(
		ctx, peer, initReq,
		func(ctx context.Context, stream io.ReadWriter) (err error) {
			return pss.requestCallback(ctx, stream, rsr, x, y)
		})
}

func (pss *PairwiseSetSyncer) Serve(ctx context.Context, stream io.ReadWriter, os OrderedSet) error {
	c := startWireConduit(ctx, stream, pss.cfg)
	defer c.Stop()
	rsr := pss.createReconciler(os)
	if err := rsr.Run(c); err != nil {
		return err
	}
	c.End()
	return nil
}

func (pss *PairwiseSetSyncer) Register(d *Dispatcher, os OrderedSet) {
	d.Register(pss.name, func(ctx context.Context, _ p2p.Peer, s io.ReadWriter) error {
		return pss.Serve(ctx, s, os)
	})
}

func (pss *PairwiseSetSyncer) Sent() int {
	return int(pss.sent.Load())
}

func (pss *PairwiseSetSyncer) Received() int {
	return int(pss.recv.Load())
}
