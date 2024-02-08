package compat

import (
	"context"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3"
)

type weakCoin interface {
	Set(types.LayerID, bool) error
}

func ReportWeakcoin(ctx context.Context, logger *zap.Logger, from <-chan hare3.WeakCoinOutput, to weakCoin) {
	for {
		select {
		case <-ctx.Done():
			logger.Info("weak coin reporter exited")
			return
		case out, open := <-from:
			if !open {
				return
			}
			if err := to.Set(out.Layer, out.Coin); err != nil {
				logger.Error("failed to update weakcoin",
					zap.Uint32("lid", out.Layer.Uint32()),
					zap.Error(err),
				)
			}
		}
	}
}
