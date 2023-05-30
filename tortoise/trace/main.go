package main

import (
	"flag"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

var (
	level = zap.LevelFlag("level", zapcore.InfoLevel, "set verbosity level for execution")
)

func main() {
	flag.Parse()
	atom := zap.NewAtomicLevelAt(*level)
	logger := log.NewWithLevel("trace", atom)
	logger.With().Debug("using trace", log.String("path", flag.Arg(0)))
	if err := tortoise.RunTrace(flag.Arg(0), tortoise.WithLogger(logger)); err != nil {
		logger.With().Fatal("run trace failed", log.Err(err))
	}
}
