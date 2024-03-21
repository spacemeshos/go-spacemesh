package activation

import (
	"context"
	"fmt"
	"io"
	"net/url"
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
		challenge *types.NIPostChallenge,
		certifier certifierService,
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

type PoetCert []byte

func (c PoetCert) Bytes() []byte {
	return c
}

type PoetAuth struct {
	*PoetPoW
	PoetCert
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

	CertifierInfo(context.Context) (*url.URL, []byte, error)

	// Proof returns the proof for the given round ID.
	Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, []types.Member, error)
}

// A certifier client that the certifierService uses to obtain certificates
// The implementation can use any method to obtain the certificate,
// for example, POST verification.
type certifierClient interface {
	// The ID for which this client certifies.
	Id() types.NodeID
	// Certify the ID in the given certifier.
	Certify(ctx context.Context, url *url.URL, pubkey []byte) (PoetCert, error)
}

// certifierService is used to certify nodeID for registerting in the poet.
// It holds the certificates and can recertify if needed.
type certifierService interface {
	// Acquire a certificate for the given poet.
	GetCertificate(poet string) PoetCert
	// Recertify the nodeID and return a certificate confirming that
	// it is verified. The certificate can be later used to submit in poet.
	Recertify(ctx context.Context, poet PoetClient) (PoetCert, error)

	// Certify the nodeID for all given poets.
	// It won't recertify poets that already have a certificate.
	// It returns a map of a poet address to a certificate for it.
	CertifyAll(ctx context.Context, poets []PoetClient) map[string]PoetCert
}

type poetDbAPI interface {
	GetProof(types.PoetProofRef) (*types.PoetProof, *types.Hash32, error)
	ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error
}

var (
	ErrPostClientClosed       = fmt.Errorf("post client closed")
	ErrPostClientNotConnected = fmt.Errorf("post service not registered")
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
