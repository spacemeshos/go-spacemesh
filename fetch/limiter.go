package fetch

import (
	"context"
)

type limiter interface {
	Acquire(ctx context.Context, n int64) error
	Release(n int64)
}

type getHashesOpts struct {
	limiter limiter
}

type noLimit struct{}

func (noLimit) Acquire(context.Context, int64) error { return nil }

func (noLimit) Release(int64) {}
