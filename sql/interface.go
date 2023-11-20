package sql

//go:generate mockgen -typed -package=sql -destination=./mocks.go -source=./interface.go

// Migration is interface for migrations provider.
type Migration interface {
	Apply(db Executor) error
	Rollback() error
	Name() string
	Order() int
}
