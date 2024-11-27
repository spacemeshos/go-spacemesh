package sql

import "go.uber.org/zap"

//go:generate mockgen -typed -package=sql -destination=./mocks.go -source=./interface.go

// Executor is an interface for executing raw statement.
type Executor interface {
	// Exec executes a statement.
	Exec(string, Encoder, Decoder) (int, error)
}

// Migration is interface for migrations provider.
type Migration interface {
	// Apply applies the migration.
	Apply(db Executor, logger *zap.Logger) error
	// Rollback rolls back the migration.
	Rollback() error
	// Name returns the name of the migration.
	Name() string
	// Order returns the sequential number of the migration.
	Order() int
}
