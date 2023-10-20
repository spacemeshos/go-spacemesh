package activation

import (
	"context"
	"io"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=activation -destination=./mocks.go -source=./interface.go

type AtxReceiver interface {
	OnAtx(*types.ActivationTxHeader)
}

type PostVerifier interface {
	io.Closer
	Verify(ctx context.Context, p *shared.Proof, m *shared.ProofMetadata, opts ...verifying.OptionFunc) error
}
type nipostValidator interface {
	InitialNIPostChallenge(challenge *types.NIPostChallenge, atxs atxProvider, goldenATXID types.ATXID) error
	NIPostChallenge(challenge *types.NIPostChallenge, atxs atxProvider, nodeID types.NodeID) error
	NIPost(ctx context.Context, nodeId types.NodeID, atxId types.ATXID, NIPost *types.NIPost, expectedChallenge types.Hash32, numUnits uint32) (uint64, error)

	NumUnits(cfg *PostConfig, numUnits uint32) error
	Post(ctx context.Context, nodeId types.NodeID, atxId types.ATXID, Post *types.Post, PostMetadata *types.PostMetadata, numUnits uint32) error
	PostMetadata(cfg *PostConfig, metadata *types.PostMetadata) error

	VRFNonce(nodeId types.NodeID, commitmentAtxId types.ATXID, vrfNonce *types.VRFPostIndex, PostMetadata *types.PostMetadata, numUnits uint32) error
	PositioningAtx(id types.ATXID, atxs atxProvider, goldenATXID types.ATXID, pubepoch types.EpochID) error
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

type nipostBuilder interface {
	UpdatePoETProvers([]poetClient)
	BuildNIPost(ctx context.Context, challenge *types.NIPostChallenge) (*types.NIPost, error)
	DataDir() string
}

type syncer interface {
	RegisterForATXSynced() chan struct{}
}

type atxProvider interface {
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
}

// PostSetupProvider defines the functionality required for Post setup.
// This interface is used by the atx builder and currently implemented by the PostSetupManager.
// Eventually most of the functionality will be moved to the PoSTClient.
type postSetupProvider interface {
	PrepareInitializer(ctx context.Context, opts PostSetupOpts) error
	StartSession(context context.Context) error
	Reset() error
	CommitmentAtx() (types.ATXID, error)
	VRFNonce() (*types.VRFPostIndex, error)
	LastOpts() *PostSetupOpts
	Config() PostConfig
}

// SmeshingProvider defines the functionality required for the node's Smesher API.
type SmeshingProvider interface {
	Smeshing() bool
	StartSmeshing(types.Address, PostSetupOpts) error
	StopSmeshing(bool) error
	SmesherID() types.NodeID
	Coinbase() types.Address
	SetCoinbase(coinbase types.Address)
	UpdatePoETServers(ctx context.Context, endpoints []string) error
}

// poetClient servers as an interface to communicate with a PoET server.
// It is used to submit challenges and fetch proofs.
type poetClient interface {
	Address() string

	PowParams(ctx context.Context) (*PoetPowParams, error)

	// Submit registers a challenge in the proving service current open round.
	Submit(ctx context.Context, deadline time.Time, prefix, challenge []byte, signature types.EdSignature, nodeID types.NodeID, pow PoetPoW) (*types.PoetRound, error)

	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID(context.Context) (types.PoetServiceID, error)

	// Proof returns the proof for the given round ID.
	Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, []types.Member, error)
}

type poetDbAPI interface {
	GetProof(types.PoetProofRef) (*types.PoetProof, *types.Hash32, error)
	ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error
}

type postService interface {
	Client(nodeId types.NodeID) (PostClient, error)
}

type PostClient interface {
	Proof(ctx context.Context, challenge []byte) (*types.Post, *types.PostMetadata, error)
}
