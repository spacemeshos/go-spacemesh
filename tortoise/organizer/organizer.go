package organizer

import (
	"context"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/system"
)

var _ system.Tortoise = (*Organizer)(nil)

// Opt is for configuring Organizer.
type Opt func(*Organizer)

// WithLogger configures logger for Organizer.
func WithLogger(logger log.Log) Opt {
	return func(o *Organizer) {
		o.logger = logger
	}
}

// WithVerifiedLayer layer configures last verified layer for organizer to return.
// Otherwise Organizer will return effective genesis.
func WithVerifiedLayer(lid types.LayerID) Opt {
	return func(o *Organizer) {
		o.verified = lid
		o.submitted = lid
		o.received = lid
	}
}

// WithWindowSize configures size of the window.
// By default window of the size 1000 is used.
func WithWindowSize(size int) Opt {
	return func(o *Organizer) {
		o.window = make([]types.LayerID, size)
	}
}

// New creates Organizer instance.
func New(tortoise system.Tortoise, opts ...Opt) *Organizer {
	genesis := types.GetEffectiveGenesis()
	o := &Organizer{
		logger:    log.NewNop(),
		Tortoise:  tortoise,
		submitted: genesis,
		received:  genesis,
		verified:  genesis,
		window:    make([]types.LayerID, 1000), // ~4kb
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Organizer is a decorator for tortoise that keeps layers in order within the window
// and sends them to tortoise only when the window has no gaps.
type Organizer struct {
	logger log.Log

	mu sync.Mutex
	system.Tortoise
	// last submitted layer
	submitted types.LayerID
	// last received layer that is not submitted
	received types.LayerID
	// all layers between submitted and received
	// window is non-empty only if there are gaps after submitted
	window []types.LayerID
	// verified is the last layer that was verified by tortoise.
	verified types.LayerID
}

// HandleIncomingLayer will send the layer to the tortoise if the layer follows previous layer.
// If there is a gap between this layer and previously received layer Organizer will keep the layer in the window.
// If we receive such layer that doesn't fit in the window Organizer will send all pending layers to tortoise
// even if they have a gap.
func (o *Organizer) HandleIncomingLayer(ctx context.Context, lid types.LayerID) (types.LayerID, types.LayerID, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !lid.After(o.submitted) {
		return o.verified, o.verified, false
	}
	var (
		oldverified, newverified = o.verified, o.verified
		reverted                 bool
	)
	if lid.After(o.submitted.Add(uint32(len(o.window)))) {
		o.logger.WithContext(ctx).With().Error("tortoise organizer received a layer that does not fit in the current window."+
			" subbmiting pending with gaps",
			log.Int("window", len(o.window)),
			log.Named("submitted", o.submitted),
			log.Named("received", lid),
		)
		oldverified, newverified, reverted = o.submitPending(ctx, true)
		o.received = lid
		o.submitted = lid.Sub(1)
	}

	// at this stage we know that the layer is within the window
	pos := int(lid.Uint32()) % len(o.window)
	checkEmpty(o.logger, o.window[pos])
	o.window[pos] = lid
	if lid.After(o.received) {
		o.received = lid
	}
	if lid.Sub(1) == o.submitted {
		old1, new1, revert1 := o.submitPending(ctx, false)
		oldverified = earliestLayer(oldverified, old1)
		newverified = latestLayer(newverified, new1)
		reverted = reverted || revert1
	} else {
		o.logger.WithContext(ctx).With().Warning("tortoise organizer has a gap in the window, not subbmiting a layer",
			log.Named("submitted", o.submitted),
			log.Named("received", lid),
		)
	}

	o.verified = newverified
	return oldverified, newverified, reverted
}

// submitPending submits all non-nil layers in (o.submitted, o.received].
func (o *Organizer) submitPending(ctx context.Context, all bool) (types.LayerID, types.LayerID, bool) {
	var (
		oldverified, newverified types.LayerID = o.verified, o.verified
		reverted                 bool
	)
	for lid := o.submitted.Add(1); !lid.After(o.received); lid = lid.Add(1) {
		pos := int(lid.Uint32()) % len(o.window)
		if (o.window[pos] == types.LayerID{}) {
			if !all {
				break
			}
			continue
		}
		o.window[pos] = types.LayerID{}
		o.submitted = lid

		old1, new1, revert1 := o.Tortoise.HandleIncomingLayer(ctx, lid)
		oldverified = earliestLayer(oldverified, old1)
		newverified = latestLayer(oldverified, new1)
		reverted = reverted || revert1
	}
	return oldverified, newverified, reverted
}

func earliestLayer(lid1, lid2 types.LayerID) types.LayerID {
	if lid1.Before(lid2) {
		return lid1
	}
	return lid2
}

func latestLayer(lid1, lid2 types.LayerID) types.LayerID {
	if lid1.After(lid2) {
		return lid1
	}
	return lid2
}

func checkEmpty(logger log.Log, lid types.LayerID) {
	if (lid == types.LayerID{}) {
		return
	}
	logger.With().Panic("invalid state. overwriting non-empty layer in the window",
		lid,
	)
}
