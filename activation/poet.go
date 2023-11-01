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
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrUnavailable    = errors.New("unavailable")
	ErrInvalidRequest = errors.New("invalid request")
	ErrUnathorized    = errors.New("unauthorized")
)

type PoetPowParams struct {
	Challenge  []byte
	Difficulty uint
}

type PoetPoW struct {
	Nonce  uint64
	Params PoetPowParams
}

// HTTPPoetClient implements PoetProvingServiceClient interface.
type HTTPPoetClient struct {
	baseURL       *url.URL
	poetServiceID types.PoetServiceID
	client        *retryablehttp.Client
	logger        *zap.Logger
}

func checkRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return true, nil
	}
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}

// A wrapper around zap.Logger to make it compatible with
// retryablehttp.LeveledLogger interface.
type retryableHttpLogger struct {
	inner *zap.Logger
}

func (r retryableHttpLogger) Error(format string, args ...any) {
	r.inner.Sugar().Errorw(format, args...)
}

func (r retryableHttpLogger) Info(format string, args ...any) {
	r.inner.Sugar().Infow(format, args...)
}

func (r retryableHttpLogger) Warn(format string, args ...any) {
	r.inner.Sugar().Warnw(format, args...)
}

func (r retryableHttpLogger) Debug(format string, args ...any) {
	r.inner.Sugar().Debugw(format, args...)
}

type PoetClientOpts func(*HTTPPoetClient)

func withCustomHttpClient(client *http.Client) PoetClientOpts {
	return func(c *HTTPPoetClient) {
		c.client.HTTPClient = client
	}
}

func WithLogger(logger *zap.Logger) PoetClientOpts {
	return func(c *HTTPPoetClient) {
		c.logger = logger
		c.client.Logger = &retryableHttpLogger{inner: logger}
		c.client.ResponseLogHook = func(logger retryablehttp.Logger, resp *http.Response) {
			c.logger.Info(
				"response received",
				zap.Stringer("url", resp.Request.URL),
				zap.Int("status", resp.StatusCode),
			)
		}
	}
}

// NewHTTPPoetClient returns new instance of HTTPPoetClient connecting to the specified url.
func NewHTTPPoetClient(baseUrl string, cfg PoetConfig, opts ...PoetClientOpts) (*HTTPPoetClient, error) {
	client := &retryablehttp.Client{
		RetryMax:     cfg.MaxRequestRetries,
		RetryWaitMin: cfg.RequestRetryDelay,
		RetryWaitMax: 2 * cfg.RequestRetryDelay,
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
		logger:  zap.NewNop(),
	}
	for _, opt := range opts {
		opt(poetClient)
	}

	poetClient.logger.Info(
		"created poet client",
		zap.Stringer("url", baseURL),
		zap.Int("max retries", client.RetryMax),
		zap.Duration("min retry wait", client.RetryWaitMin),
		zap.Duration("max retry wait", client.RetryWaitMax),
	)

	return poetClient, nil
}

func (c *HTTPPoetClient) Address() string {
	return c.baseURL.String()
}

func (c *HTTPPoetClient) PowParams(ctx context.Context) (*PoetPowParams, error) {
	resBody := rpcapi.PowParamsResponse{}
	if err := c.req(ctx, http.MethodGet, "/v1/pow_params", nil, &resBody); err != nil {
		return nil, fmt.Errorf("querying PoW params: %w", err)
	}

	return &PoetPowParams{
		Challenge:  resBody.GetPowParams().GetChallenge(),
		Difficulty: uint(resBody.GetPowParams().GetDifficulty()),
	}, nil
}

func (c *HTTPPoetClient) CertifierInfo(ctx context.Context) (*CertifierInfo, error) {
	info, err := c.info(ctx)
	if err != nil {
		return nil, err
	}
	certifierInfo := info.GetCertifier()
	if certifierInfo == nil {
		return nil, errors.New("poet doesn't support certifier")
	}
	url, err := url.Parse(certifierInfo.Url)
	if err != nil {
		return nil, fmt.Errorf("parsing certifier address: %w", err)
	}
	return &CertifierInfo{
		PubKey: certifierInfo.Pubkey,
		URL:    url,
	}, nil
}

// Submit registers a challenge in the proving service current open round.
func (c *HTTPPoetClient) Submit(
	ctx context.Context,
	deadline time.Time,
	prefix, challenge []byte,
	signature types.EdSignature,
	nodeID types.NodeID,
	auth PoetAuth,
) (*types.PoetRound, error) {
	request := rpcapi.SubmitRequest{
		Prefix:    prefix,
		Challenge: challenge,
		Signature: signature.Bytes(),
		Pubkey:    nodeID.Bytes(),
		Deadline:  timestamppb.New(deadline),
	}
	if auth.PoetPoW != nil {
		request.PowParams = &rpcapi.PowParams{
			Challenge:  auth.PoetPoW.Params.Challenge,
			Difficulty: uint32(auth.PoetPoW.Params.Difficulty),
		}
		request.Nonce = auth.PoetPoW.Nonce
	}
	if auth.PoetCert != nil {
		request.Certificate = &rpcapi.SubmitRequest_Certificate{
			Signature: auth.PoetCert.Signature,
		}
	}

	resBody := rpcapi.SubmitResponse{}
	if err := c.req(ctx, http.MethodPost, "/v1/submit", &request, &resBody); err != nil {
		return nil, fmt.Errorf("submitting challenge: %w", err)
	}
	roundEnd := time.Time{}
	if resBody.RoundEnd != nil {
		roundEnd = time.Now().Add(resBody.RoundEnd.AsDuration())
	}

	return &types.PoetRound{ID: resBody.RoundId, End: types.RoundEnd(roundEnd)}, nil
}

func (c *HTTPPoetClient) info(ctx context.Context) (*rpcapi.InfoResponse, error) {
	resBody := rpcapi.InfoResponse{}
	if err := c.req(ctx, http.MethodGet, "/v1/info", nil, &resBody); err != nil {
		return nil, fmt.Errorf("getting poet ID: %w", err)
	}
	// cache the poet service ID
	c.poetServiceID.ServiceID = resBody.ServicePubkey
	return &resBody, nil
}

// PoetServiceID returns the public key of the PoET proving service.
func (c *HTTPPoetClient) PoetServiceID(ctx context.Context) (types.PoetServiceID, error) {
	if c.poetServiceID.ServiceID != nil {
		return c.poetServiceID, nil
	}
	if _, err := c.info(ctx); err != nil {
		return types.PoetServiceID{}, err
	}

	return c.poetServiceID, nil
}

// Proof implements PoetProvingServiceClient.
func (c *HTTPPoetClient) Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, []types.Member, error) {
	resBody := rpcapi.ProofResponse{}
	if err := c.req(ctx, http.MethodGet, fmt.Sprintf("/v1/proofs/%s", roundID), nil, &resBody); err != nil {
		return nil, nil, fmt.Errorf("getting proof: %w", err)
	}

	p := resBody.Proof.GetProof()

	pMembers := resBody.Proof.GetMembers()
	members := make([]types.Member, len(pMembers))
	for i, m := range pMembers {
		copy(members[i][:], m)
	}
	statement, err := calcRoot(members)
	if err != nil {
		return nil, nil, fmt.Errorf("calculating root: %w", err)
	}

	proof := types.PoetProofMessage{
		PoetProof: types.PoetProof{
			MerkleProof: shared.MerkleProof{
				Root:         p.GetRoot(),
				ProvenLeaves: p.GetProvenLeaves(),
				ProofNodes:   p.GetProofNodes(),
			},
			LeafCount: resBody.Proof.GetLeaves(),
		},
		PoetServiceID: resBody.Pubkey,
		RoundID:       roundID,
		Statement:     types.BytesToHash(statement),
	}
	if c.poetServiceID.ServiceID == nil {
		c.poetServiceID.ServiceID = proof.PoetServiceID
	}

	return &proof, members, nil
}

func (c *HTTPPoetClient) req(ctx context.Context, method, path string, reqBody, resBody proto.Message) error {
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

	if res.StatusCode != http.StatusOK {
		c.logger.Info("got poet response != 200 OK", zap.String("status", res.Status), zap.String("body", string(data)))
	}

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return fmt.Errorf("%w: response status code: %s, body: %s", ErrNotFound, res.Status, string(data))
	case http.StatusServiceUnavailable:
		return fmt.Errorf("%w: response status code: %s, body: %s", ErrUnavailable, res.Status, string(data))
	case http.StatusBadRequest:
		return fmt.Errorf("%w: response status code: %s, body: %s", ErrInvalidRequest, res.Status, string(data))
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: response status code: %s, body: %s", ErrUnathorized, res.Status, string(data))
	default:
		return fmt.Errorf("unrecognized error: status code: %s, body: %s", res.Status, string(data))
	}

	if resBody != nil {
		unmarshaler := protojson.UnmarshalOptions{DiscardUnknown: true}
		if err := unmarshaler.Unmarshal(data, resBody); err != nil {
			return fmt.Errorf("decoding response body to proto: %w", err)
		}
	}

	return nil
}
