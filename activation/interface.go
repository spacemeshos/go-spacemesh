package activation

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

//go:generate mockgen -typed -package=activation -destination=./mocks.go -source=./interface.go

type AtxReceiver interface {
	OnAtx(*types.ActivationTxHeader)
}

type PostVerifier interface {
	io.Closer
	Verify(ctx context.Context, p *shared.Proof, m *shared.ProofMetadata, opts ...verifying.OptionFunc) error
}

type scaler interface {
	scale(int)
}

// validatorOption is a functional option type for the validator.
type validatorOption func(*validatorOptions)

type nipostValidator interface {
	InitialNIPostChallenge(challenge *types.NIPostChallenge, atxs atxProvider, goldenATXID types.ATXID) error
	NIPostChallenge(challenge *types.NIPostChallenge, atxs atxProvider, nodeID types.NodeID) error
	NIPost(
		ctx context.Context,
		nodeId types.NodeID,
		commitmentAtxId types.ATXID,
		NIPost *types.NIPost,
		expectedChallenge types.Hash32,
		numUnits uint32,
		opts ...validatorOption,
	) (uint64, error)

	NumUnits(cfg *PostConfig, numUnits uint32) error

	IsVerifyingFullPost() bool

	Post(
		ctx context.Context,
		nodeId types.NodeID,
		commitmentAtxId types.ATXID,
		Post *types.Post,
		PostMetadata *types.PostMetadata,
		numUnits uint32,
		opts ...validatorOption,
	) error
	PostMetadata(cfg *PostConfig, metadata *types.PostMetadata) error

	VRFNonce(
		nodeId types.NodeID,
		commitmentAtxId types.ATXID,
		vrfNonce *types.VRFPostIndex,
		PostMetadata *types.PostMetadata,
		numUnits uint32,
	) error
	PositioningAtx(id types.ATXID, atxs atxProvider, goldenATXID types.ATXID, pubepoch types.EpochID) error

	// VerifyChain fully verifies all dependencies of the given ATX and the ATX itself.
	VerifyChain(ctx context.Context, id, goldenATXID types.ATXID, opts ...VerifyChainOption) error
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

type nipostBuilder interface {
	BuildNIPost(
		ctx context.Context,
		sig *signing.EdSigner,
		publish types.EpochID,
		challenge types.Hash32,
	) (*nipost.NIPostState, error)
	Proof(ctx context.Context, nodeID types.NodeID, challenge []byte) (*types.Post, *types.PostInfo, error)
	ResetState(types.NodeID) error
}

type syncer interface {
	RegisterForATXSynced() <-chan struct{}
}

type atxProvider interface {
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
}

// PostSetupProvider defines the functionality required for Post setup.
// This interface is used by the atx builder and currently implemented by the PostSetupManager.
// Eventually most of the functionality will be moved to the PoSTClient.
type postSetupProvider interface {
	PrepareInitializer(ctx context.Context, opts PostSetupOpts, id types.NodeID) error
	StartSession(context context.Context, id types.NodeID) error
	Status() *PostSetupStatus
	Reset() error
}

// SmeshingProvider defines the functionality required for the node's Smesher API.
type SmeshingProvider interface {
	Smeshing() bool
	StartSmeshing(types.Address) error
	StopSmeshing(bool) error
	SmesherIDs() []types.NodeID
	Coinbase() types.Address
	SetCoinbase(coinbase types.Address)
}

// poetClient servers as an interface to communicate with a PoET server.
// It is used to submit challenges and fetch proofs.
type poetClient interface {
	Address() string

	// Submit registers a challenge in the proving service current open round.
	Submit(
		ctx context.Context,
		deadline time.Time,
		prefix, challenge []byte,
		signature types.EdSignature,
		nodeID types.NodeID,
	) (*types.PoetRound, error)

	// Proof returns the proof for the given round ID.
	Proof(ctx context.Context, roundID string) (*types.PoetProof, []types.Hash32, error)
}

type poetDbAPI interface {
	Proof(types.PoetProofRef) (*types.PoetProof, *types.Hash32, error)
	ProofForRound(poetID []byte, roundID string) (*types.PoetProof, error)
	ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error
}

var (
	ErrPostClientClosed       = errors.New("post client closed")
	ErrPostClientNotConnected = errors.New("post service not registered")
)

type AtxBuilder interface {
	Register(sig *signing.EdSigner)
}

type postService interface {
	Client(nodeId types.NodeID) (PostClient, error)
}

type PostClient interface {
	Info(ctx context.Context) (*types.PostInfo, error)
	Proof(ctx context.Context, challenge []byte) (*types.Post, *types.PostInfo, error)
}

type PostStates interface {
	Set(id types.NodeID, state types.PostState)
	Get() map[types.NodeID]types.PostState
}
