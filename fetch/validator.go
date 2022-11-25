package fetch

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
)

var (
	errStopped     = errors.New("stopped")
	stoppedPromise = &promise{waiter: make(chan struct{})}
)

func init() {
	stoppedPromise.set(errStopped)
}

func newValidator(fetcher *Fetch) *validator {
	ctx, cancel := context.WithCancel(context.Background())
	return &validator{
		promises: map[key]*promise{},
		ctx:      ctx,
		cancel:   cancel,
		fetcher:  fetcher,
	}
}

type key struct {
	hint datastore.Hint
	id   types.Hash32
}

type promise struct {
	result error
	waiter chan struct{}
}

func (p *promise) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.waiter:
		return p.result
	}
}

func (p *promise) set(result error) {
	p.result = result
	close(p.waiter)
}

type validator struct {
	mu       sync.Mutex
	promises map[key]*promise
	ctx      context.Context
	cancel   func()
	eg       errgroup.Group

	fetcher *Fetch
}

func (v *validator) requestHash(id types.Hash32, hint datastore.Hint, receiver dataReceiver) *promise {
	v.mu.Lock()
	defer v.mu.Unlock()
	select {
	case <-v.ctx.Done():
		return stoppedPromise
	default:
	}
	k := key{hint: hint, id: id}
	p := v.promises[k]
	if p == nil {
		p = &promise{waiter: make(chan struct{})}
		v.promises[k] = p
		v.eg.Go(func() error {
			result := v.fetcher.GetHash(id, hint, false)
			select {
			case <-v.ctx.Done():
				p.set(v.ctx.Err())
			case rst := <-result:
				if rst.Err != nil {
					p.set(rst.Err)
				} else if rst.IsLocal {
					p.set(nil)
				} else if rst.Data != nil {
					p.set(receiver(v.ctx, rst.Data))
				} else {
					p.set(nil)
				}
			}
			v.mu.Lock()
			delete(v.promises, k)
			v.mu.Unlock()
			return nil
		})
	}
	return p
}

func (v *validator) Stop() {
	v.mu.Lock()
	v.cancel()
	v.mu.Unlock()
	v.eg.Wait()
}
