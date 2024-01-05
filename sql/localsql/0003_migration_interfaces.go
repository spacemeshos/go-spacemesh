package localsql

import (
	"context"
)

//go:generate mockgen -typed -package=localsql -destination=./0003_mocks.go -source=./0003_migration_interfaces.go

type PoetClient interface {
	PoetServiceID(ctx context.Context) PoetServiceID
	Address() string
}
