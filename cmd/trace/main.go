package main

import (
	"flag"
	"runtime"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

var (
	level  = zap.LevelFlag("level", zapcore.ErrorLevel, "set verbosity level for execution")
	bpoint = flag.Bool("breakpoint", false, "enable breakpoint after every step")
)

func main() {
	flag.Parse()
	atom := zap.NewAtomicLevelAt(*level)
	logger := log.NewWithLevel("trace", atom)
	logger.With().Debug("using trace", log.String("path", flag.Arg(0)))
	var breakpoint func()
	if *bpoint {
		breakpoint = runtime.Breakpoint
	}
	if err := tortoise.RunTrace(flag.Arg(0), breakpoint, tortoise.WithLogger(logger)); err != nil {
		logger.With().Fatal("run trace failed", log.Err(err))
	}
}
