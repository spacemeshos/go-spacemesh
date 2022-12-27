package activation

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=activation -destination=./mocks.go -source=./interface.go

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
	ValidateVRFNonce(nodeId types.NodeID, commitmentAtxId types.ATXID, vrfNonce *types.VRFPostIndex, PostMetadata *types.PostMetadata, numUnits uint32) error
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

type nipostBuilder interface {
	updatePoETProvers([]PoetProvingServiceClient)
	BuildNIPost(ctx context.Context, challenge *types.PoetChallenge, poetProofDeadline time.Time) (*types.NIPost, time.Duration, error)
}

type atxHandler interface {
	GetPosAtxID() (types.ATXID, error)
	AwaitAtx(id types.ATXID) chan struct{}
	UnsubscribeAtx(id types.ATXID)
}

type signer interface {
	Sign(m []byte) []byte
}

type keyExtractor interface {
	ExtractNodeID(m, sig []byte) (types.NodeID, error)
}

type syncer interface {
	RegisterForATXSynced() chan struct{}
}

type atxProvider interface {
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
}

// PostSetupProvider defines the functionality required for Post setup.
type postSetupProvider interface {
	Status() *PostSetupStatus
	ComputeProviders() []PostSetupComputeProvider
	Benchmark(p PostSetupComputeProvider) (int, error)
	StartSession(context context.Context, opts PostSetupOpts, commitmentAtx types.ATXID) error
	Reset() error
	GenerateProof(challenge []byte) (*types.Post, *types.PostMetadata, error)
	VRFNonce() (*types.VRFPostIndex, error)
	LastOpts() *PostSetupOpts
	Config() PostConfig
}
