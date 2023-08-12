package blockssync

import (
	"context"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
)

func Sync(ctx context.Context, logger *zap.Logger, requests <-chan map[types.BlockID]struct{}, fetcher *fetch.Fetch) error {
	var (
		eg         errgroup.Group
		aggregated = make(chan map[types.BlockID]struct{})
	)
	eg.Go(func() error {
		var aggregate map[types.BlockID]struct{}
		for {
			select {
			case <-ctx.Done():
				close(aggregated)
				return ctx.Err()
			case req := <-requests:
				if aggregate == nil {
					aggregate = req
				} else {
					for id := range req {
						aggregate[id] = struct{}{}
					}
				}
			case aggregated <- aggregate:
				aggregate = nil
			}
		}
	})
	for batch := range aggregated {
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
