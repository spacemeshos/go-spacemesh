package activation

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/interface.go -source=./interface.go

type poetValidatorPersister interface {
	HasProof(types.PoetProofRef) bool
	Validate(types.PoetProof, []byte, string, []byte) error
	StoreProof(types.PoetProofRef, *types.PoetProofMessage) error
}

type nipostValidator interface {
	Validate(commitment []byte, NIPost *types.NIPost, expectedChallenge types.Hash32, numUnits uint) (uint64, error)
	ValidatePost(commitment []byte, Post *types.Post, PostMetadata *types.PostMetadata, numUnits uint) error
}

type layerClock interface {
	AwaitLayer(ctx context.Context, layerID types.LayerID) context.Context
	GetCurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}
