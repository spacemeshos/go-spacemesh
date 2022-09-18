package activation

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type poetValidatorPersister interface {
	HasProof([]byte) bool
	Validate(types.PoetProof, []byte, string, []byte) error
	StoreProof([]byte, *types.PoetProofMessage) error
}

type nipostValidator interface {
	Validate(commitment []byte, NIPost *types.NIPost, expectedChallenge types.Hash32, numUnits uint) (uint64, error)
	ValidatePost(commitment []byte, Post *types.Post, PostMetadata *types.PostMetadata, numUnits uint) error
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}
