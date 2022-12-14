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
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

type nipostBuilder interface {
	updatePoETProvers([]PoetProvingServiceClient)
	BuildNIPost(ctx context.Context, challenge *types.PoetChallenge, commitmentAtx types.ATXID, poetProofDeadline time.Time) (*types.NIPost, time.Duration, error)
}

type atxHandler interface {
	GetPosAtxID() (types.ATXID, error)
	AwaitAtx(id types.ATXID) chan struct{}
	UnsubscribeAtx(id types.ATXID)
}

type signer interface {
	Sign(m []byte) []byte
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
	StartSession(opts PostSetupOpts, commitmentAtx types.ATXID) (chan struct{}, error)
	StopSession(deleteFiles bool) error
	GenerateProof(challenge []byte, commitmentAtx types.ATXID) (*types.Post, *types.PostMetadata, error)
	LastError() error
	LastOpts() *PostSetupOpts
	Config() PostConfig
}
