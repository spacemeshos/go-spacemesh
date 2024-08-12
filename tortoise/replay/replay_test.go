package replay

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

var (
	dbpath = flag.String("dbpath", "", "path to database")
	level  = zap.LevelFlag("level", zapcore.InfoLevel, "set log level")
)

func TestReplayMainnet(t *testing.T) {
	if len(*dbpath) == 0 {
		t.Skip("set -dbpath=<PATH> to run this test")
	}

	cfg := config.MainnetConfig()
	types.SetLayersPerEpoch(cfg.LayersPerEpoch)

	logger := zaptest.NewLogger(t, zaptest.Level(*level)).Named("replay")
	opts := []tortoise.Opt{
		tortoise.WithLogger(logger),
		tortoise.WithConfig(cfg.Tortoise),
	}

	genesis, err := time.Parse(time.RFC3339, cfg.Genesis.GenesisTime)
	require.NoError(t, err)
	clock, err := timesync.NewClock(
		timesync.WithLayerDuration(cfg.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(genesis),
		timesync.WithLogger(zap.NewNop()),
	)
	require.NoError(t, err)

	db, err := sql.Open(fmt.Sprintf("file:%s?mode=ro", *dbpath))
	require.NoError(t, err)

	applied, err := layers.GetLastApplied(db)
	require.NoError(t, err)

	start := time.Now()
	atxsdata, err := atxsdata.Warm(db, cfg.Tortoise.WindowSizeEpochs(applied), logger)
	require.NoError(t, err)
	trtl, err := tortoise.Recover(
		context.Background(),
		db,
		atxsdata,
		clock.CurrentLayer(), opts...,
	)
	require.NoError(t, err)
	updates := trtl.Updates()
	logger.Info(
		"initialized",
		zap.Duration("duration", time.Since(start)),
		zap.Array("updates", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
			for _, rst := range updates {
				encoder.AppendObject(&rst)
			}
			return nil
		})),
	)
}
