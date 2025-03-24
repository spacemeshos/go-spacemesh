package node

import (
	"fmt"

	"github.com/go-viper/mapstructure/v2"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/config"
)

type loggers struct {
	levels map[string]*zap.AtomicLevel
}

func newLoggers(cfg *config.LoggerConfig) (*loggers, error) {
	logLevels := map[string]string{}
	if err := mapstructure.Decode(cfg, &logLevels); err != nil {
		return nil, fmt.Errorf("error decoding mapstructure: %w", err)
	}
	delete(logLevels, "log-encoder")

	levels := make(map[string]*zap.AtomicLevel, len(logLevels))
	for name, levelStr := range logLevels {
		var lvl zap.AtomicLevel
		if err := lvl.UnmarshalText([]byte(levelStr)); err != nil {
			return nil, fmt.Errorf("unmarshaling zap log level: %w", err)
		}
		levels[name] = &lvl
	}

	return &loggers{levels}, nil
}

func (l *loggers) add(name string, base *zap.Logger) *zap.Logger {
	lvl, ok := l.levels[name]
	if !ok {
		newLvl := zap.NewAtomicLevel()
		lvl = &newLvl
		l.levels[name] = lvl
	}

	return base.WithOptions(zap.IncreaseLevel(lvl)).Named(name)
}
