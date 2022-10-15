package system

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/tortoise.go -source=./tortoise.go

// DecodedBallot contains all information required by tortoise.
type DecodedBallot interface {
	Opinion() types.Hash32
	CancelWeight()
	Store() error
}

// Tortoise is an interface provided by tortoise implementation.
type Tortoise interface {
	DecodeBallot(*types.Ballot) (DecodedBallot, error)
	OnBlock(*types.Block)
	OnHareOutput(types.LayerID, types.BlockID)
	TallyVotes(context.Context, types.LayerID)
	LatestComplete() types.LayerID
}
