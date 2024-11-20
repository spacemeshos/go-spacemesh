package fetch

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type limiter interface {
	Acquire(ctx context.Context, n int64) error
	Release(n int64)
}

type getHashesOpts struct {
	limiter  limiter
	callback func(types.Hash32, error)
}

type noLimit struct{}

func (noLimit) Acquire(context.Context, int64) error { return nil }

func (noLimit) Release(int64) {}
