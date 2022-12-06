package activation

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=activation -destination=./interface_mocks.go -source=./interface.go

type atxReceiver interface {
	OnAtx(*types.ActivationTxHeader)
}

type poetValidatorPersister interface {
	HasProof(types.PoetProofRef) bool
	Validate(types.PoetProof, []byte, string, []byte) error
	StoreProof(context.Context, types.PoetProofRef, *types.PoetProofMessage) error
}

type nipostValidator interface {
	Validate(nodeId types.NodeID, atxId types.ATXID, NIPost *types.NIPost, expectedChallenge types.Hash32, numUnits uint32) (uint64, error)
	ValidatePost(nodeId types.NodeID, atxId types.ATXID, Post *types.Post, PostMetadata *types.PostMetadata, numUnits uint32) error
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}
