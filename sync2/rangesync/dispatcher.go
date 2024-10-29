package rangesync

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

// Handler is a function that handles a request for a Dispatcher.
type Handler func(ctx context.Context, s io.ReadWriter) error

// Dispatcher multiplexes a P2P Server to multiple set reconcilers.
type Dispatcher struct {
	*server.Server
	mtx      sync.Mutex
	logger   *zap.Logger
	handlers map[string]Handler
}

// NewDispatcher creates a new Dispatcher.
func NewDispatcher(logger *zap.Logger) *Dispatcher {
	return &Dispatcher{
		logger:   logger,
		handlers: make(map[string]Handler),
	}
}

// SetupServer creates a new P2P Server for the Dispatcher.
func (d *Dispatcher) SetupServer(
	host host.Host,
	proto string,
	opts ...server.Opt,
) *server.Server {
	d.Server = server.New(host, proto, d.Dispatch, opts...)
	return d.Server
}

// Register registers a handler with a Dispatcher.
func (d *Dispatcher) Register(name string, h Handler) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	d.handlers[name] = h
}

// Dispatch dispatches a request to a handler.
func (d *Dispatcher) Dispatch(
	ctx context.Context,
	req []byte,
	stream io.ReadWriter,
) (err error) {
	name := string(req)
	d.mtx.Lock()
	h, ok := d.handlers[name]
	d.mtx.Unlock()
	if !ok {
		return fmt.Errorf("no handler named %q", name)
	}
	d.logger.Debug("dispatch", zap.String("handler", name))
	if err := h(ctx, stream); err != nil {
		return fmt.Errorf("handler %q: %w", name, err)
	}
	return nil
}
