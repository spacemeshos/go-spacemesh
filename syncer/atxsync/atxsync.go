package atxsync

import (
	"context"
	"math/rand/v2"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/system"
)

func getMissing(db *sql.Database, set []types.ATXID) ([]types.ATXID, error) {
	missing := []types.ATXID{}
	for _, atx := range set {
		exist, err := atxs.Has(db, atx)
		if err != nil {
			return nil, err
		}
		if !exist {
			missing = append(missing, atx)
		}
	}
	return missing, nil
}

// Download specified set of atxs from peers in the network.
//
// actual retry interval will be between [retryInterval, 2*retryInterval].
func Download(
	ctx context.Context,
	retryInterval time.Duration,
	logger *zap.Logger,
	db *sql.Database,
	fetcher system.AtxFetcher,
	set []types.ATXID,
) error {
	total := len(set)
	for {
		missing, err := getMissing(db, set)
		if err != nil {
			return err
		}
		set = missing
		downloaded := total - len(missing)
		logger.Info("downloaded atxs",
			zap.Int("total", total),
			zap.Int("downloaded", downloaded),
			zap.Array("missing", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
				for _, atx := range missing {
					enc.AppendString(atx.ShortString())
				}
				return nil
			})))
		if len(missing) == 0 {
			return nil
		}
		if err := fetcher.GetAtxs(ctx, missing); err != nil {
			logger.Debug("failed to fetch atxs", zap.Error(err))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval + rand.N(retryInterval)):
			}
		}
	}
}
