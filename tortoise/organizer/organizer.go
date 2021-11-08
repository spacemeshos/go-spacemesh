package organizer

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// LayersIterator is used to traverse organized layers.
// For siplicity LayersIterator iterates over all layers unconditionally.
type LayersIterator func(types.LayerID)

// Opt is for configuring Organizer.
type Opt func(*Organizer)

// WithLogger configures logger for Organizer.
func WithLogger(logger log.Log) Opt {
	return func(o *Organizer) {
		o.logger = logger
	}
}

// WithLastLayer layer configures last layer that tortoise knows about.
func WithLastLayer(lid types.LayerID) Opt {
	return func(o *Organizer) {
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
func New(opts ...Opt) *Organizer {
	o := &Organizer{
		logger: log.NewNop(),
		window: make([]types.LayerID, 1000), // ~4kb
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

	// last submitted layer
	submitted types.LayerID
	// last received layer that is not submitted
	received types.LayerID
	// all layers between submitted and received
	// window is non-empty only if there are gaps after submitted
	window []types.LayerID
}

// Iterate allows to iterate over all ordered layers that are currently within the window including new layer.
func (o *Organizer) Iterate(ctx context.Context, lid types.LayerID, f LayersIterator) {
	if !lid.After(o.submitted) {
		return
	}
	if lid.After(o.submitted.Add(uint32(len(o.window)))) {
		o.logger.WithContext(ctx).With().Error("tortoise organizer received a layer that does not fit in the current window."+
			" subbmiting pending with gaps",
			log.Int("window", len(o.window)),
			log.Named("submitted", o.submitted),
			log.Named("received", lid),
		)
		o.submitPending(ctx, true, f)
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
		o.submitPending(ctx, false, f)
	} else {
		o.logger.WithContext(ctx).With().Warning("tortoise organizer has a gap in the window, not subbmiting a layer",
			log.Named("submitted", o.submitted),
			log.Named("received", lid),
		)
	}
}

// submitPending submits all non-nil layers in (o.submitted, o.received].
func (o *Organizer) submitPending(ctx context.Context, all bool, f LayersIterator) {
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
		f(lid)
	}
}

func checkEmpty(logger log.Log, lid types.LayerID) {
	if (lid == types.LayerID{}) {
		return
	}
	logger.With().Panic("invalid state. overwriting non-empty layer in the window",
		lid,
	)
}
