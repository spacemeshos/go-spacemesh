package activation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/spacemeshos/poet/config"
	rpcapi "github.com/spacemeshos/poet/release/proto/go/rpc/api/v1"
	"github.com/spacemeshos/poet/server"
	"github.com/spacemeshos/poet/shared"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrUnavailable = errors.New("unavailable")
)

// HTTPPoetHarness utilizes a local self-contained poet server instance
// targeted by an HTTP client, in order to exercise functionality.
type HTTPPoetHarness struct {
	*HTTPPoetClient
	Service *server.Server
}

type HTTPPoetOpt func(*config.Config)

func WithGateway(endpoint string) HTTPPoetOpt {
	return func(cfg *config.Config) {
		cfg.Service.GatewayAddresses = []string{endpoint}
	}
}

func WithGenesis(genesis time.Time) HTTPPoetOpt {
	return func(cfg *config.Config) {
		cfg.Service.Genesis = genesis.Format(time.RFC3339)
	}
}

func WithEpochDuration(epoch time.Duration) HTTPPoetOpt {
	return func(cfg *config.Config) {
		cfg.Service.EpochDuration = epoch
	}
}

func WithPhaseShift(phase time.Duration) HTTPPoetOpt {
	return func(cfg *config.Config) {
		cfg.Service.PhaseShift = phase
	}
}

func WithCycleGap(gap time.Duration) HTTPPoetOpt {
	return func(cfg *config.Config) {
		cfg.Service.CycleGap = gap
	}
}

// NewHTTPPoetHarness returns a new instance of HTTPPoetHarness.
func NewHTTPPoetHarness(ctx context.Context, poetdir string, opts ...HTTPPoetOpt) (*HTTPPoetHarness, error) {
	cfg := config.DefaultConfig()
	cfg.PoetDir = poetdir
	cfg.RawRESTListener = "localhost:0"

	for _, opt := range opts {
		opt(cfg)
	}

	cfg, err := config.SetupConfig(cfg)
	if err != nil {
		return nil, err
	}

	poet, err := server.New(ctx, *cfg)
	if err != nil {
		return nil, err
	}

	return &HTTPPoetHarness{
		HTTPPoetClient: NewHTTPPoetClient(poet.GrpcRestProxyAddr().String(), PoetConfig{
			PhaseShift: cfg.Service.PhaseShift,
			CycleGap:   cfg.Service.CycleGap,
		}),
		Service: poet,
	}, nil
}

// HTTPPoetClient implements PoetProvingServiceClient interface.
type HTTPPoetClient struct {
	baseURL       string
	poetServiceID *types.PoetServiceID
	client        *retryablehttp.Client
}

func defaultPoetClientFunc(target string, cfg PoetConfig) PoetProvingServiceClient {
	return NewHTTPPoetClient(target, cfg)
}

// NewHTTPPoetClient returns new instance of HTTPPoetClient for the specified target.
func NewHTTPPoetClient(target string, cfg PoetConfig) *HTTPPoetClient {
	client := retryablehttp.NewClient()
	client.RetryMax = 10
	client.RetryWaitMin = cfg.CycleGap / 100
	client.RetryWaitMax = cfg.CycleGap / 10
	client.Backoff = retryablehttp.LinearJitterBackoff

	// Retry on 404 in addition to the default retry policy.
	// Reasoning: The proof or round data might not be available YET.
	client.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if retry, err := retryablehttp.DefaultRetryPolicy(ctx, resp, err); retry || err != nil {
			return retry, err
		}

		if resp.StatusCode == http.StatusNotFound {
			return true, nil
		}
		return false, nil
	}

	return &HTTPPoetClient{
		baseURL: fmt.Sprintf("http://%s/v1", target),
		client:  client,
	}
}

// Start is an administrative endpoint of the proving service that tells it to start. This is mostly done in tests,
// since it requires administrative permissions to the proving service.
func (c *HTTPPoetClient) Start(ctx context.Context, gatewayAddresses []string) error {
	reqBody := rpcapi.StartRequest{GatewayAddresses: gatewayAddresses}
	if err := c.req(ctx, "POST", "/start", &reqBody, nil); err != nil {
		return fmt.Errorf("starting poet: %w", err)
	}

	return nil
}

// Submit registers a challenge in the proving service current open round.
func (c *HTTPPoetClient) Submit(ctx context.Context, challenge []byte, signature []byte) (*types.PoetRound, error) {
	request := rpcapi.SubmitRequest{
		Challenge: challenge,
		Signature: signature,
	}
	resBody := rpcapi.SubmitResponse{}
	if err := c.req(ctx, "POST", "/submit", &request, &resBody); err != nil {
		return nil, fmt.Errorf("submitting challenge: %w", err)
	}
	roundEnd := time.Time{}
	if resBody.RoundEnd != nil {
		roundEnd = time.Now().Add(resBody.RoundEnd.AsDuration())
	}
	if len(resBody.Hash) != types.Hash32Length {
		return nil, fmt.Errorf("invalid hash len (%d instead of %d)", len(resBody.Hash), types.Hash32Length)
	}
	hash := types.Hash32{}
	hash.SetBytes(resBody.Hash)
	return &types.PoetRound{ID: resBody.RoundId, ChallengeHash: hash, End: types.RoundEnd(roundEnd)}, nil
}

// PoetServiceID returns the public key of the PoET proving service.
func (c *HTTPPoetClient) PoetServiceID(ctx context.Context) (types.PoetServiceID, error) {
	if c.poetServiceID != nil {
		return *c.poetServiceID, nil
	}
	resBody := rpcapi.GetInfoResponse{}

	if err := c.req(ctx, "GET", "/info", nil, &resBody); err != nil {
		return nil, fmt.Errorf("getting poet ID: %w", err)
	}

	id := types.PoetServiceID(resBody.ServicePubkey)
	c.poetServiceID = &id
	return id, nil
}

// GetProof implements PoetProvingServiceClient.
func (c *HTTPPoetClient) GetProof(ctx context.Context, roundID string) (*types.PoetProofMessage, error) {
	resBody := rpcapi.GetProofResponse{}

	if err := c.req(ctx, "GET", fmt.Sprintf("/proofs/%s", roundID), nil, &resBody); err != nil {
		return nil, fmt.Errorf("getting proof: %w", err)
	}

	proof := types.PoetProofMessage{
		PoetProof: types.PoetProof{
			MerkleProof: shared.MerkleProof{
				Root:         resBody.Proof.GetProof().GetRoot(),
				ProvenLeaves: resBody.Proof.GetProof().GetProvenLeaves(),
				ProofNodes:   resBody.Proof.GetProof().GetProofNodes(),
			},
			Members:   resBody.Proof.GetMembers(),
			LeafCount: resBody.Proof.GetLeaves(),
		},
		PoetServiceID: resBody.Pubkey,
		RoundID:       roundID,
	}
	if c.poetServiceID == nil {
		c.poetServiceID = &proof.PoetServiceID
	}

	return &proof, nil
}

func (c *HTTPPoetClient) req(ctx context.Context, method string, endURL string, reqBody proto.Message, resBody proto.Message) error {
	jsonReqBody, err := protojson.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request body: %w", err)
	}

	url := fmt.Sprintf("%s%s", c.baseURL, endURL)
	req, err := retryablehttp.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(jsonReqBody))
	if err != nil {
		return fmt.Errorf("creating HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("doing request: %w", err)
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("reading response body (%w)", err)
	}

	log.GetLogger().WithContext(ctx).With().Debug("response from poet", log.String("status", res.Status), log.String("body", string(data)))

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return fmt.Errorf("%w: response status code: %s, body: %s", ErrNotFound, res.Status, string(data))
	case http.StatusServiceUnavailable:
		return fmt.Errorf("%w: response status code: %s, body: %s", ErrUnavailable, res.Status, string(data))
	default:
		return fmt.Errorf("unrecognized error: status code: %s, body: %s", res.Status, string(data))
	}

	if resBody != nil {
		if err := protojson.Unmarshal(data, resBody); err != nil {
			return fmt.Errorf("decoding response body to proto: %w", err)
		}
	}

	return nil
}
