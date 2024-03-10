package replay

import (
	"context"
	"flag"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/migration_extension"
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

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

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
		timesync.WithLogger(log.NewNop().Zap()),
	)
	require.NoError(t, err)

	db, err := sql.Open(fmt.Sprintf("file:%s", *dbpath),
		sql.WithMigration(migration_extension.M0017()))
	require.NoError(t, err)

	start := time.Now()
	atxsdata, err := atxsdata.Warm(db,
		atxsdata.WithCapacityFromLayers(cfg.Tortoise.WindowSize, cfg.LayersPerEpoch),
	)
	require.NoError(t, err)
	zlog.Info("loaded atxs data", zap.Duration("duration", time.Since(start)))
	trtl, err := tortoise.Recover(
		context.Background(),
		db,
		atxsdata,
		clock.CurrentLayer(), opts...,
	)
	require.NoError(t, err)

	// run it after recovery, but before updates, as update may result in eviction from memory
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	updates := trtl.Updates()

	zlog.Info(
		"initialized",
		zap.Duration("duration", time.Since(start)),
		zap.Stringer("mode", trtl.Mode()),
		zap.Float64("heap", float64(after.HeapInuse-before.HeapInuse)/1024/1024),
		zap.Array("updates", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			for _, rst := range updates {
				encoder.AppendObject(&rst)
			}
			return nil
		})),
	)
}
