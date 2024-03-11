package main

import (
	"context"
	"flag"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/txs"
)

var (
	dbpath = flag.String(
		"db",
		"",
		"database path. create a copy of the database prior to running this tool, as it may not get into the previous state",
	)
	from = flag.Int(
		"from",
		0,
		"layer to rerun from. with 0 we will always start from the database that has only genesis accounts",
	)
	level = zap.LevelFlag("level", zapcore.InfoLevel, "set log level")
)

func main() {
	flag.Parse()
	if len(*dbpath) == 0 {
		panic("set -db=<PATH> to run this test")
	}

	logger := log.NewWithLevel("replay", zap.NewAtomicLevelAt(*level))
	zlog := logger.Zap()

	db, err := sql.Open("file:" + *dbpath)
	must(zlog, err)

	mainnet := config.MainnetConfig()
	types.SetLayersPerEpoch(mainnet.LayersPerEpoch)
	cfg := vm.DefaultConfig()
	cfg.GasLimit = mainnet.BlockGasLimit
	cfg.GenesisID = mainnet.Genesis.GenesisID()
	vm := vm.New(db, vm.WithConfig(cfg), vm.WithLogger(logger))
	data := atxsdata.New()
	conservative := txs.NewConservativeState(vm, db,
		txs.WithCSConfig(txs.CSConfig{
			BlockGasLimit:     mainnet.BlockGasLimit,
			NumTXsPerProposal: mainnet.TxsPerProposal,
		}),
		txs.WithLogger(logger),
	)
	executor := mesh.NewExecutor(
		db,
		data,
		vm,
		conservative,
		logger,
	)

	from := max(types.LayerID(*from), types.GetEffectiveGenesis()+1)
	end, err := layers.GetLastApplied(db)
	must(zlog, err)
	zlog.Info("executing rerun between layers", zap.Uint32("start", uint32(from)), zap.Uint32("end", uint32(end)))
	must(zlog, executor.Revert(context.Background(), from))
	start := time.Now()
	zlog.Info("loaded atxs",
		zap.Uint32("evicted", data.Evicted().Uint32()),
		zap.Duration("duration", time.Since(start)),
	)

	for lid := from; lid <= end; lid++ {
		if lid.FirstInEpoch() {
			must(zlog, atxsdata.Load(db, data, lid.GetEpoch()-1))
		}
		applied, err := layers.GetApplied(db, lid)
		var block *types.Block
		if err == nil && applied != types.EmptyBlockID {
			block, err = blocks.Get(db, applied)
			must(zlog, err)
		}
		must(zlog, executor.Execute(context.Background(), lid, block))
		data.OnEpoch(lid.GetEpoch())
	}
}

func must(logger *zap.Logger, err error) {
	if err != nil {
		logger.Error("rerun failed", zap.Error(err))
		os.Exit(1)
	}
}
