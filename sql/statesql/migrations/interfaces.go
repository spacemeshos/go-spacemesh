package migrations

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
)

//go:generate mockgen -typed -package=migrations -destination=./mocks.go -source=interfaces.go

type malfeasanceValidator interface {
	Validate(ctx context.Context, proof *wire.MalfeasanceProof) (types.NodeID, error)
}
