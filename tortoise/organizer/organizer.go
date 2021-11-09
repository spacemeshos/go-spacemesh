package organizer

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Callback is used to send iterated layers to the caller.
// For siplicity Callback iterates over all layers unconditionally.
type Callback func(types.LayerID)

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
		o.buf = make(layerBuffer, size)
	}
}

// New creates Organizer instance.
func New(opts ...Opt) *Organizer {
	o := &Organizer{
		logger: log.NewNop(),
		buf:    make(layerBuffer, 1000), // ~4kb
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
	// buf stores all layers between submitted and received
	// buf is non-empty only if there are gaps after submitted
	buf layerBuffer
}

// Iterate allows to iterate over all ordered layers that are currently within the window including new layer.
func (o *Organizer) Iterate(ctx context.Context, lid types.LayerID, f Callback) {
	if !lid.After(o.submitted) {
		return
	}
	if lid.After(o.submitted.Add(uint32(len(o.buf)))) {
		o.logger.WithContext(ctx).With().Error("tortoise organizer received a layer that does not fit in the current window."+
			" subbmiting pending with gaps",
			log.Int("window", len(o.buf)),
			log.Named("submitted", o.submitted),
			log.Named("received", lid),
		)
		o.submitAll(f)
		// after all layers are submitted update submitted to the layer before this one
		o.submitted = lid.Sub(1)
	}

	// at this stage we know that the layer is within the window
	o.buf.store(lid)
	if lid.After(o.received) {
		o.received = lid
	}
	if lid.Sub(1) == o.submitted {
		o.submitPending(f)
	} else {
		o.logger.WithContext(ctx).With().Warning("tortoise organizer has a gap in the window, not subbmiting a layer",
			log.Named("submitted", o.submitted),
			log.Named("received", lid),
		)
	}
}

// submitAll is a special case that submits all layers in a buffer even if there are gaps between them.
func (o *Organizer) submitAll(f Callback) {
	for lid := o.submitted.Add(1); !lid.After(o.received); lid = lid.Add(1) {
		_, exists := o.buf.pop(lid)
		if !exists {
			continue
		}
		o.submitted = lid
		f(lid)
	}
}

// submitPending submits all non-nil layers in (o.submitted, o.received].
func (o *Organizer) submitPending(f Callback) {
	for lid := o.submitted.Add(1); !lid.After(o.received); lid = lid.Add(1) {
		_, exists := o.buf.pop(lid)
		if !exists {
			return
		}
		o.submitted = lid
		f(lid)
	}
}

type layerBuffer []types.LayerID

func (buf layerBuffer) store(lid types.LayerID) {
	pos := int(lid.Uint32()) % len(buf)
	checkEmpty(buf[pos])
	buf[pos] = lid
}

func (buf layerBuffer) pop(lid types.LayerID) (types.LayerID, bool) {
	pos := int(lid.Uint32()) % len(buf)
	rst := buf[pos]
	buf[pos] = types.LayerID{}
	return rst, rst != types.LayerID{}
}

func checkEmpty(lid types.LayerID) {
	if (lid == types.LayerID{}) {
		return
	}
	panic(fmt.Sprintf("invalid state. overwriting non-empty layer %s in the window", lid))
}
