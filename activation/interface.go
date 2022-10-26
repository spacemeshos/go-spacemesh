package activation

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/interface.go -source=./interface.go

type atxReceiver interface {
	OnAtx(*types.ActivationTxHeader)
}

type poetValidatorPersister interface {
	HasProof(types.PoetProofRef) bool
	Validate(types.PoetProof, []byte, string, []byte) error
	StoreProof(types.PoetProofRef, *types.PoetProofMessage) error
}

type nipostValidator interface {
	Validate(commitment []byte, NIPost *types.NIPost, expectedChallenge types.Hash32, numUnits uint32) (uint64, error)
	ValidatePost(commitment []byte, Post *types.Post, PostMetadata *types.PostMetadata, numUnits uint32) error
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}
