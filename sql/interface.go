package sql

import "go.uber.org/zap"

//go:generate mockgen -typed -package=sql -destination=./mocks.go -source=./interface.go

// Executor is an interface for executing raw statement.
type Executor interface {
	Exec(string, Encoder, Decoder) (int, error)
}

// Migration is interface for migrations provider.
type Migration interface {
	Apply(db Executor, logger *zap.Logger) error
	Rollback() error
	Name() string
	Order() int
}
