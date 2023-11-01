package activation

import (
	"context"
	"io"
	"net/url"
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
	NIPost(
		ctx context.Context,
		nodeId types.NodeID,
		atxId types.ATXID,
		NIPost *types.NIPost,
		expectedChallenge types.Hash32,
		numUnits uint32,
	) (uint64, error)

	NumUnits(cfg *PostConfig, numUnits uint32) error
	Post(
		ctx context.Context,
		nodeId types.NodeID,
		atxId types.ATXID,
		Post *types.Post,
		PostMetadata *types.PostMetadata,
		numUnits uint32,
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
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

type nipostBuilder interface {
	BuildNIPost(ctx context.Context, challenge *types.NIPostChallenge, certifier certifierService) (*types.NIPost, error)
	DataDir() string
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
	PrepareInitializer(opts PostSetupOpts) error
	StartSession(context context.Context) error
	Status() *PostSetupStatus
	Reset() error
}

// SmeshingProvider defines the functionality required for the node's Smesher API.
type SmeshingProvider interface {
	Smeshing() bool
	StartSmeshing(types.Address) error
	StopSmeshing(bool) error
	SmesherID() types.NodeID
	Coinbase() types.Address
	SetCoinbase(coinbase types.Address)
}

type PoetCert struct {
	Signature []byte
}

type PoetAuth struct {
	*PoetPoW
	*PoetCert
}

// PoetClient servers as an interface to communicate with a PoET server.
// It is used to submit challenges and fetch proofs.
type PoetClient interface {
	Address() string

	// FIXME: remove support for deprecated poet PoW
	PowParams(ctx context.Context) (*PoetPowParams, error)

	// FIXME: remove support for deprecated poet PoW
	// Submit registers a challenge in the proving service current open round.
	Submit(
		ctx context.Context,
		deadline time.Time,
		prefix, challenge []byte,
		signature types.EdSignature,
		nodeID types.NodeID,
		auth PoetAuth,
	) (*types.PoetRound, error)

	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID(context.Context) (types.PoetServiceID, error)

	CertifierInfo(context.Context) (*CertifierInfo, error)

	// Proof returns the proof for the given round ID.
	Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, []types.Member, error)
}

type CertifierInfo struct {
	URL    *url.URL
	PubKey []byte
}

// certifierService is used to certify nodeID for registerting in the poet.
type certifierService interface {
	// Acquire a certificate for the given poet.
	GetCertificate(poet string) *PoetCert
	// Recertify the nodeID and return a certificate confirming that
	// it is verified. The certificate can be later used to submit in poet.
	Recertify(ctx context.Context, poet PoetClient) (*PoetCert, error)

	// Certify the nodeID for all given poets.
	// It won't recertify poets that already have a certificate.
	// It returns a map of a poet address to a certificate for it.
	CertifyAll(ctx context.Context, poets []PoetClient) map[string]PoetCert
}

type poetDbAPI interface {
	GetProof(types.PoetProofRef) (*types.PoetProof, *types.Hash32, error)
	ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error
}

type postService interface {
	Client(nodeId types.NodeID) (PostClient, error)
}

type PostClient interface {
	Info(ctx context.Context) (*types.PostInfo, error)
	Proof(ctx context.Context, challenge []byte) (*types.Post, *types.PostInfo, error)
}
