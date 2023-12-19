package atxsync

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type atxFetcher interface {
	GetAtxs(context.Context, []types.ATXID) error
}

// Download specified set of atxs from peers in the network.
func Download(
	ctx context.Context,
	logger *zap.Logger,
	data *atxsdata.Data,
	fetcher atxFetcher,
	epoch types.EpochID,
	set []types.ATXID,
) {
	for {
		_, missing := data.WeightForSet(epoch, set)
		n := 0
		for i := range missing {
			if missing[i] {
				n++
			}
		}
		if n == 0 {
			logger.Info("all requested atxs are downloaded",
				epoch.Field().Zap(),
				zap.Array("atxs", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
					for i := range set {
						enc.AppendString(set[i].ShortString())
					}
					return nil
				})))
			return
		}
		requsted := make([]types.ATXID, 0, n)
		for i := range missing {
			if missing[i] {
				requsted = append(requsted, set[i])
			}
		}
		set = requsted
		logger.Debug("downloading atxs", epoch.Field().Zap(),
			zap.Array("atxs", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
				for i := range set {
					enc.AppendString(set[i].ShortString())
				}
				return nil
			})))
		if err := fetcher.GetAtxs(ctx, set); err != nil {
			// TODO add small wait on error
			logger.Debug("failed to fetch atxs", zap.Error(err))
		}
	}

}
