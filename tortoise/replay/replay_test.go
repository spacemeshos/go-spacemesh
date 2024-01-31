package replay

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

	log.JSONLog(true)
	logger := log.NewWithLevel("replay", zap.NewAtomicLevelAt(*level))
	zlog := logger.Zap()
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
		timesync.WithLogger(log.NewNop()),
	)
	require.NoError(t, err)

	db, err := sql.Open("file:" + *dbpath)
	require.NoError(t, err)

	start := time.Now()
	trtl, err := tortoise.Recover(
		context.Background(),
		db,
		clock.CurrentLayer(), opts...,
	)
	require.NoError(t, err)
	updates := trtl.Updates()
	zlog.Info(
		"initialized",
		zap.Duration("duration", time.Since(start)),
		zap.Array("updates", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			for _, rst := range updates {
				encoder.AppendObject(&rst)
			}
			return nil
		})),
	)
}
