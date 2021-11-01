package tortoise

import (
	"context"
	"fmt"
	"time"
	"unsafe"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type rerunResult struct {
	Consensus *turtle
	Tracer    *validityTracer
}

func (t *ThreadSafeVerifyingTortoise) updateFromRerun(ctx context.Context) (bool, types.LayerID) {
	var (
		logger   = t.logger.WithContext(ctx).With()
		reverted bool
		observed types.LayerID
	)
	if value := t.update.Load(); value != nil {
		var (
			completed = (*rerunResult)(value)
			current   = t.trtl
			updated   = completed.Consensus
			err       error
		)
		if current.Last.After(updated.Last) {
			start := time.Now()
			logger.Info("tortoise received more layers while rerun was in progress. running a catchup",
				log.FieldNamed("last", current.Last),
				log.FieldNamed("rerun-last", updated.Last))

			err = catchupToCurrent(ctx, current, updated)
			if err != nil {
				logger.Error("cathup failed", log.Err(err))
			} else {
				logger.Info("catchup finished", log.Duration("duration", time.Since(start)))
			}
		}
		if err == nil {
			reverted = completed.Tracer.Reverted()
			if reverted {
				observed = completed.Tracer.FirstLayer()
			}
			updated.log = current.log
			updated.bdp = current.bdp
			t.trtl = updated
		}
		t.update.CAS(value, nil) // Store will miss a concurrent rerun
	}
	return reverted, observed
}

func (t *ThreadSafeVerifyingTortoise) waitRerun(ctx context.Context, period time.Duration) {
	timer := time.NewTimer(period)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_ = t.rerun(ctx) // err is already logged with additional context
			timer.Reset(period)
		}
	}
}

func (t *ThreadSafeVerifyingTortoise) rerun(ctx context.Context) error {
	t.mu.RLock()
	last := t.trtl.Last
	t.mu.RUnlock()

	logger := t.logger.WithContext(ctx).WithFields(
		log.Bool("rerun", true),
		log.FieldNamed("last-layer-id", last),
	)

	start := time.Now()
	logger.With().Info("tortoise rerun started")

	consensus := t.trtl.cloneTurtleParams()
	consensus.logger = logger
	consensus.init(ctx, mesh.GenesisLayer())
	tracer := &validityTracer{blockDataProvider: consensus.bdp}
	consensus.bdp = tracer

	for lid := types.GetEffectiveGenesis(); !lid.After(last); lid = lid.Add(1) {
		if err := consensus.HandleIncomingLayer(ctx, lid); err != nil {
			logger.With().Error("tortoise rerun failed", log.Err(err))
			return err
		}
	}
	if tracer.Reverted() {
		logger = logger.WithFields(log.FieldNamed("first-reverted-layer", tracer.FirstLayer()))
	}
	logger.With().Info("tortoise rerun completed", last, log.Duration("duration", time.Since(start)))

	rst := &rerunResult{Consensus: consensus, Tracer: tracer}
	t.update.Store((unsafe.Pointer)(rst))
	return nil
}

// validityTracer monitors the tortoise rerun for database changes that would cause us to need to revert state.
type validityTracer struct {
	blockDataProvider
	firstUpdatedLayer *types.LayerID
}

// SaveContextualValidity overrides the method in the embedded type to check if we've made changes.
func (vt *validityTracer) SaveContextualValidity(bid types.BlockID, lid types.LayerID, validityNew bool) error {
	if vt.firstUpdatedLayer == nil {
		validityCur, err := vt.ContextualValidity(bid)
		if err != nil {
			return fmt.Errorf("error reading contextual validity of block %v: %w", bid, err)
		}
		if validityCur != validityNew {
			vt.firstUpdatedLayer = &lid
		}
	}
	if err := vt.blockDataProvider.SaveContextualValidity(bid, lid, validityNew); err != nil {
		return fmt.Errorf("save contextual validity: %w", err)
	}
	return nil
}

func (vt *validityTracer) Reverted() bool {
	return vt.firstUpdatedLayer != nil
}

// FirstLayer should be called only if Reverted returns true.
func (vt *validityTracer) FirstLayer() types.LayerID {
	return *vt.firstUpdatedLayer
}

func catchupToCurrent(ctx context.Context, current, updated *turtle) error {
	for lid := updated.Last.Add(1); !lid.After(current.Last); lid = lid.Add(1) {
		if err := updated.HandleIncomingLayer(ctx, lid); err != nil {
			return err
		}
	}
	return nil
}
