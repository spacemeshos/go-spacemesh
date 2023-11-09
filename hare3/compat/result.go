package compat

import (
	"context"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare3"
)

func ReportResult(ctx context.Context, logger *zap.Logger, from <-chan hare3.ConsensusOutput, to chan<- hare.LayerOutput) {
	for {
		select {
		case <-ctx.Done():
			logger.Info("hare3 results reporter exited")
			return
		case out, open := <-from:
			if !open {
				return
			}
			select {
			case to <- hare.LayerOutput{
				Ctx:       ctx,
				Layer:     out.Layer,
				Proposals: out.Proposals,
			}:
			case <-ctx.Done():
			}
		}
	}
}
