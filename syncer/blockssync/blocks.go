package blockssync

import (
	"context"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type blockFetcher interface {
	GetBlocks(context.Context, []types.BlockID) error
}

// Sync requests last specified blocks in background.
func Sync(ctx context.Context, logger *zap.Logger, requests <-chan []types.BlockID, fetcher blockFetcher) error {
	var (
		eg     errgroup.Group
		lastch = make(chan []types.BlockID)
	)
	eg.Go(func() error {
		var (
			send chan []types.BlockID
			last []types.BlockID
		)
		for {
			select {
			case <-ctx.Done():
				close(lastch)
				return ctx.Err()
			case req := <-requests:
				if last == nil {
					send = lastch
				}
				last = req
			case send <- last:
				last = nil
				send = nil
			}
		}
	})
	for batch := range lastch {
		if err := fetcher.GetBlocks(ctx, batch); err != nil {
			logger.Warn("failed to fetch blocks", zap.Error(err))
		}
	}
	return eg.Wait()
}
