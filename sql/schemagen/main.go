package main

import (
	"flag"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

var (
	level  = zap.LevelFlag("level", zapcore.ErrorLevel, "set log verbosity level")
	dbType = flag.String("dbtype", "state", "database type (state, local, default state)")
	output = flag.String("output", "", "output file (defaults to stdout)")
)

func main() {
	var (
		err    error
		schema *sql.Schema
	)
	flag.Parse()
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewProductionEncoderConfig()),
		os.Stderr,
		zap.NewAtomicLevelAt(*level),
	)
	logger := zap.New(core).With(zap.String("dbType", *dbType))
	switch *dbType {
	case "state":
		schema, err = statesql.Schema()
	case "local":
		schema, err = localsql.Schema()
	default:
		logger.Fatal("unknown database type, must be state or local")
	}
	if err != nil {
		logger.Fatal("error loading db schema", zap.Error(err))
	}
	g := sql.NewSchemaGen(logger, schema)
	if err := g.Generate(*output); err != nil {
		logger.Fatal("error generating schema", zap.Error(err), zap.String("output", *output))
	}
}
