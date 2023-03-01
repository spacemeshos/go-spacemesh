package activation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	rpcapi "github.com/spacemeshos/poet/release/proto/go/rpc/api/v1"
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

// HTTPPoetClient implements PoetProvingServiceClient interface.
type HTTPPoetClient struct {
	baseURL       *url.URL
	poetServiceID types.PoetServiceID
	client        *retryablehttp.Client
}

func defaultPoetClientFunc(address string, cfg PoetConfig) (PoetProvingServiceClient, error) {
	return NewHTTPPoetClient(address, cfg)
}

func checkRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if retry, err := retryablehttp.DefaultRetryPolicy(ctx, resp, err); retry || err != nil {
		return retry, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return true, nil
	}
	return false, nil
}

type PoetClientOpts func(*HTTPPoetClient)

func withCustomHttpClient(client *http.Client) PoetClientOpts {
	return func(c *HTTPPoetClient) {
		c.client.HTTPClient = client
	}
}

// NewHTTPPoetClient returns new instance of HTTPPoetClient connecting to the specified url.
func NewHTTPPoetClient(baseUrl string, cfg PoetConfig, opts ...PoetClientOpts) (*HTTPPoetClient, error) {
	client := &retryablehttp.Client{
		RetryMax:     10,
		RetryWaitMin: cfg.CycleGap / 100,
		RetryWaitMax: cfg.CycleGap / 10,
		Backoff:      retryablehttp.LinearJitterBackoff,
		CheckRetry:   checkRetry,
	}

	baseURL, err := url.Parse(baseUrl)
	if err != nil {
		return nil, fmt.Errorf("parsing address: %w", err)
	}
	if baseURL.Scheme == "" {
		baseURL.Scheme = "http"
	}

	poetClient := &HTTPPoetClient{
		baseURL: baseURL,
		client:  client,
	}
	for _, opt := range opts {
		opt(poetClient)
	}

	return poetClient, nil
}

// Start is an administrative endpoint of the proving service that tells it to start. This is mostly done in tests,
// since it requires administrative permissions to the proving service.
func (c *HTTPPoetClient) Start(ctx context.Context, gatewayAddresses []string) error {
	reqBody := rpcapi.StartRequest{GatewayAddresses: gatewayAddresses}
	if err := c.req(ctx, http.MethodPost, "/v1/start", &reqBody, nil); err != nil {
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
	if err := c.req(ctx, http.MethodPost, "/v1/submit", &request, &resBody); err != nil {
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
		return c.poetServiceID, nil
	}
	resBody := rpcapi.GetInfoResponse{}

	if err := c.req(ctx, http.MethodGet, "/v1/info", nil, &resBody); err != nil {
		return nil, fmt.Errorf("getting poet ID: %w", err)
	}

	c.poetServiceID = types.PoetServiceID(resBody.ServicePubkey)
	return c.poetServiceID, nil
}

// Proof implements PoetProvingServiceClient.
func (c *HTTPPoetClient) Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, error) {
	resBody := rpcapi.GetProofResponse{}
	if err := c.req(ctx, http.MethodGet, fmt.Sprintf("/v1/proofs/%s", roundID), nil, &resBody); err != nil {
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
		c.poetServiceID = proof.PoetServiceID
	}

	return &proof, nil
}

func (c *HTTPPoetClient) req(ctx context.Context, method string, path string, reqBody proto.Message, resBody proto.Message) error {
	jsonReqBody, err := protojson.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request body: %w", err)
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, method, c.baseURL.JoinPath(path).String(), jsonReqBody)
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
