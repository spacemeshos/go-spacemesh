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

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var dbpath = flag.String("dbpath", "", "path to database")

func TestReplayMainnet(t *testing.T) {
	if len(*dbpath) == 0 {
		t.Skip("set -dbpath=<PATH> to run this test")
	}

	cfg := config.MainnetConfig()
	types.SetLayersPerEpoch(cfg.LayersPerEpoch)

	log.JSONLog(true)
	logger := log.NewWithLevel("replay", zap.NewAtomicLevelAt(zapcore.InfoLevel))
	zlog := logger.Zap()
	opts := []tortoise.Opt{
		tortoise.WithLogger(logger),
		tortoise.WithConfig(cfg.Tortoise),
	}

	genesis, err := time.Parse(time.RFC3339, cfg.Genesis.GenesisTime)
	must(err, zlog)
	clock, err := timesync.NewClock(
		timesync.WithLayerDuration(cfg.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(genesis),
		timesync.WithLogger(log.NewNop()),
	)
	must(err, zlog)

	db, err := sql.Open("file:" + *dbpath)
	must(err, zlog)

	start := time.Now()
	trtl, err := tortoise.Recover(
		context.Background(),
		db,
		clock.CurrentLayer(), opts...,
	)
	since := time.Since(start)
	must(err, zlog)
	updates := trtl.Updates()
	zlog.Info(
		"initialized",
		zap.Duration("duration", since),
		zap.Array("updates", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			for _, rst := range updates {
				encoder.AppendObject(&rst)
			}
			return nil
		})),
	)
}

func must(err error, logger *zap.Logger) {
	if err != nil {
		logger.Fatal("error", zap.Error(err))
	}
}
