package localsql

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=localsql -destination=./0003_mocks.go -source=./0003_migration_interfaces.go

type poetClient interface {
	PoetServiceID(ctx context.Context) (types.PoetServiceID, error)
	Address() (string, error)
}
