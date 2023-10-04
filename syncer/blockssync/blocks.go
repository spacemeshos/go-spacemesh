package blockssync

import (
	"context"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=blockssync -destination=./mocks.go -source=./blocks.go

type blockFetcher interface {
	GetBlocks(context.Context, []types.BlockID) error
}

// Sync requests last specified blocks in background.
func Sync(ctx context.Context, logger *zap.Logger, requests <-chan []types.BlockID, fetcher blockFetcher) error {
	var (
		eg     errgroup.Group
		lastch = make(chan map[types.BlockID]struct{})
	)
	eg.Go(func() error {
		var (
			send chan map[types.BlockID]struct{}
			last map[types.BlockID]struct{}
		)
		for {
			select {
			case <-ctx.Done():
				close(lastch)
				return ctx.Err()
			case req := <-requests:
				if last == nil {
					last = map[types.BlockID]struct{}{}
					send = lastch
				}
				for _, id := range req {
					last[id] = struct{}{}
				}
			case send <- last:
				last = nil
				send = nil
			}
		}
	})
	for batch := range lastch {
		blocks := make([]types.BlockID, 0, len(batch))
		for id := range batch {
			blocks = append(blocks, id)
		}
		if err := fetcher.GetBlocks(ctx, blocks); err != nil {
			logger.Warn("failed to fetch blocks", zap.Error(err))
		}
	}
	return eg.Wait()
}
