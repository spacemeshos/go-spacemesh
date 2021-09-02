package taskgroup

import (
	"context"
	"errors"
	"sync"
)

// ErrTerminated returned if Group was already terminated.
var ErrTerminated = errors.New("taskgroup: terminated")

// Option to modify Group.
type Option func(g *Group)

// WithContext passes parent context to use for the Group.
func WithContext(ctx context.Context) Option {
	return func(g *Group) {
		g.ctx = ctx
	}
}

// New returns instance of the Group.
func New(opts ...Option) *Group {
	g := &Group{
		waitErr: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(g)
	}
	if g.ctx == nil {
		g.ctx = context.Background()
	}
	g.ctx, g.cancel = context.WithCancel(g.ctx)
	return g
}

// Group manages set of tasks.
// Unlike errgroup.Group it is safe to call Go after Wait. If Group didn't terminate yet
// the behavior will be the same as for errgroup.Group, if it did Go will exit immediatly.
//
// Zero value is not a valid Group. Must be initialized using New.
type Group struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	err      error
	waitErr  chan struct{}
	waitOnce bool
	wg       sync.WaitGroup
}

// Go spawns new goroutine that will run f, unless Group was already terminated.
// In the latter case ErrTerminated error will be returned.
func (g *Group) Go(f func(ctx context.Context) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.err != nil {
		return ErrTerminated
	}
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		err := f(g.ctx)

		g.mu.Lock()
		defer g.mu.Unlock()
		if err != nil && g.err == nil {
			g.err = err
			close(g.waitErr)
			g.cancel()
		}
	}()
	return nil
}

// Wait until Group is terminated. First error is returned.
func (g *Group) Wait() error {
	g.mu.Lock()
	err := g.err
	// wait at least once even if error was already set when we Wait is called for the first time
	// otherwise we can't guarantee that all goroutines are closed when Wait exits
	once := g.waitOnce
	if !once {
		g.waitOnce = true
	}
	g.mu.Unlock()
	if err != nil && once {
		return err
	}

	<-g.waitErr
	// at this point the error is set to a non-nil value, won't be ever overwritten,
	// and all Go calls will be terminated immediately, therefore re-locking here is not required.
	g.wg.Wait()
	return g.err
}
