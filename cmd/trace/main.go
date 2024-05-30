package main

import (
	"flag"
	"os"
	"runtime"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/tortoise"
)

var (
	level  = zap.LevelFlag("level", zapcore.ErrorLevel, "set verbosity level for execution")
	bpoint = flag.Bool("breakpoint", false, "enable breakpoint after every step")
)

func main() {
	flag.Parse()
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewProductionEncoderConfig()),
		os.Stdout,
		zap.NewAtomicLevelAt(*level),
	)
	logger := zap.New(core)
	logger.Debug("using trace", zap.String("path", flag.Arg(0)))
	var breakpoint func()
	if *bpoint {
		breakpoint = runtime.Breakpoint
	}
	if err := tortoise.RunTrace(flag.Arg(0), breakpoint, tortoise.WithLogger(logger)); err != nil {
		logger.Fatal("run trace failed", zap.Error(err))
	}
}
