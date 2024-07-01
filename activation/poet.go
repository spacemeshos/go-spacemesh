package activation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	rpcapi "github.com/spacemeshos/poet/release/proto/go/rpc/api/v1"
	"github.com/spacemeshos/poet/shared"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
)

var (
	ErrInvalidRequest           = errors.New("invalid request")
	ErrUnauthorized             = errors.New("unauthorized")
	errCertificatesNotSupported = errors.New("poet doesn't support certificates")
)

type PoetPowParams struct {
	Challenge  []byte
	Difficulty uint
}

type PoetPoW struct {
	Nonce  uint64
	Params PoetPowParams
}

type PoetAuth struct {
	*PoetPoW
	*certifier.PoetCert
}

type PoetClient interface {
	Id() []byte
	Address() string

	PowParams(ctx context.Context) (*PoetPowParams, error)
	CertifierInfo(ctx context.Context) (*url.URL, []byte, error)
	Submit(
		ctx context.Context,
		deadline time.Time,
		prefix, challenge []byte,
		signature types.EdSignature,
		nodeID types.NodeID,
		auth PoetAuth,
	) (*types.PoetRound, error)
	Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, []types.Hash32, error)
}

// HTTPPoetClient implements PoetProvingServiceClient interface.
type HTTPPoetClient struct {
	id      []byte
	baseURL *url.URL
	client  *retryablehttp.Client
	logger  *zap.Logger
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
func NewHTTPPoetClient(server types.PoetServer, cfg PoetConfig, opts ...PoetClientOpts) (*HTTPPoetClient, error) {
	client := &retryablehttp.Client{
		RetryMax:     cfg.MaxRequestRetries,
		RetryWaitMin: cfg.RequestRetryDelay,
		RetryWaitMax: 2 * cfg.RequestRetryDelay,
		Backoff:      retryablehttp.LinearJitterBackoff,
		CheckRetry:   checkRetry,
	}

	baseURL, err := url.Parse(server.Address)
	if err != nil {
		return nil, fmt.Errorf("parsing address: %w", err)
	}
	if baseURL.Scheme == "" {
		baseURL.Scheme = "http"
	}

	poetClient := &HTTPPoetClient{
		id:      server.Pubkey.Bytes(),
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
		zap.Binary("pubkey", server.Pubkey.Bytes()),
		zap.Int("max retries", client.RetryMax),
		zap.Duration("min retry wait", client.RetryWaitMin),
		zap.Duration("max retry wait", client.RetryWaitMax),
	)

	return poetClient, nil
}

func (c *HTTPPoetClient) Id() []byte {
	return c.id
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

func (c *HTTPPoetClient) CertifierInfo(ctx context.Context) (*url.URL, []byte, error) {
	info, err := c.info(ctx)
	if err != nil {
		return nil, nil, err
	}
	certifierInfo := info.GetCertifier()
	if certifierInfo == nil {
		return nil, nil, errCertificatesNotSupported
	}
	url, err := url.Parse(certifierInfo.Url)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing certifier address: %w", err)
	}
	return url, certifierInfo.Pubkey, nil
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
			Data:      auth.PoetCert.Data,
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

	return &types.PoetRound{ID: resBody.RoundId, End: roundEnd}, nil
}

func (c *HTTPPoetClient) info(ctx context.Context) (*rpcapi.InfoResponse, error) {
	resBody := rpcapi.InfoResponse{}
	if err := c.req(ctx, http.MethodGet, "/v1/info", nil, &resBody); err != nil {
		return nil, fmt.Errorf("getting poet info: %w", err)
	}
	return &resBody, nil
}

// Proof implements PoetProvingServiceClient.
func (c *HTTPPoetClient) Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, []types.Hash32, error) {
	resBody := rpcapi.ProofResponse{}
	if err := c.req(ctx, http.MethodGet, fmt.Sprintf("/v1/proofs/%s", roundID), nil, &resBody); err != nil {
		return nil, nil, fmt.Errorf("getting proof: %w", err)
	}

	p := resBody.Proof.GetProof()

	pMembers := resBody.Proof.GetMembers()
	members := make([]types.Hash32, len(pMembers))
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
	case http.StatusBadRequest:
		return fmt.Errorf("%w: response status code: %s, body: %s", ErrInvalidRequest, res.Status, string(data))
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: response status code: %s, body: %s", ErrUnauthorized, res.Status, string(data))
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

// poetService is a higher-level interface to communicate with a PoET service.
// It wraps the HTTP client, adding additional functionality.
type poetService struct {
	db             poetDbAPI
	logger         *zap.Logger
	client         PoetClient
	requestTimeout time.Duration

	// Used to avoid concurrent requests for proof.
	gettingProof sync.Mutex
	// cached members of the last queried proof
	proofMembers map[string][]types.Hash32

	certifier certifierService
}

type PoetClientOpt func(*poetService)

func WithCertifier(certifier certifierService) PoetClientOpt {
	return func(c *poetService) {
		c.certifier = certifier
	}
}

func NewPoetService(
	db poetDbAPI,
	server types.PoetServer,
	cfg PoetConfig,
	logger *zap.Logger,
	opts ...PoetClientOpt,
) (*poetService, error) {
	client, err := NewHTTPPoetClient(server, cfg, WithLogger(logger))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP poet client %s: %w", server.Address, err)
	}
	return NewPoetServiceWithClient(
		db,
		client,
		cfg,
		logger,
		opts...,
	), nil
}

func NewPoetServiceWithClient(
	db poetDbAPI,
	client PoetClient,
	cfg PoetConfig,
	logger *zap.Logger,
	opts ...PoetClientOpt,
) *poetService {
	poetClient := &poetService{
		db:             db,
		logger:         logger,
		client:         client,
		requestTimeout: cfg.RequestTimeout,
		proofMembers:   make(map[string][]types.Hash32, 1),
	}

	for _, opt := range opts {
		opt(poetClient)
	}

	return poetClient
}

func (c *poetService) Address() string {
	return c.client.Address()
}

func (c *poetService) authorize(
	ctx context.Context,
	nodeID types.NodeID,
	challenge []byte,
	logger *zap.Logger,
) (*PoetAuth, error) {
	logger.Debug("certifying node")
	cert, err := c.Certify(ctx, nodeID)
	switch {
	case err == nil:
		return &PoetAuth{PoetCert: cert}, nil
	case errors.Is(err, errCertificatesNotSupported):
		logger.Debug("poet doesn't support certificates")
	default:
		logger.Warn("failed to certify", zap.Error(err))
	}
	// Fallback to PoW
	// TODO: remove this fallback once we migrate to certificates fully.

	logger.Debug("querying for poet pow parameters")
	powCtx, cancel := withConditionalTimeout(ctx, c.requestTimeout)
	defer cancel()
	powParams, err := c.client.PowParams(powCtx)
	if err != nil {
		return nil, &PoetSvcUnstableError{msg: "failed to get PoW params", source: err}
	}

	logger.Debug("doing pow with params", zap.Any("pow_params", powParams))
	startTime := time.Now()
	nonce, err := shared.FindSubmitPowNonce(
		ctx,
		powParams.Challenge,
		challenge,
		nodeID.Bytes(),
		powParams.Difficulty,
	)
	metrics.PoetPowDuration.Set(float64(time.Since(startTime).Nanoseconds()))
	if err != nil {
		return nil, fmt.Errorf("running poet PoW: %w", err)
	}

	return &PoetAuth{PoetPoW: &PoetPoW{
		Nonce:  nonce,
		Params: *powParams,
	}}, nil
}

func (c *poetService) Submit(
	ctx context.Context,
	deadline time.Time,
	prefix, challenge []byte,
	signature types.EdSignature,
	nodeID types.NodeID,
) (*types.PoetRound, error) {
	logger := c.logger.With(
		log.ZContext(ctx),
		zap.String("poet", c.Address()),
		log.ZShortStringer("smesherID", nodeID),
	)

	// Try obtain a certificate
	auth, err := c.authorize(ctx, nodeID, challenge, logger)
	if err != nil {
		return nil, fmt.Errorf("authorizing: %w", err)
	}

	logger.Debug("submitting challenge to poet proving service")

	submitCtx, cancel := withConditionalTimeout(ctx, c.requestTimeout)
	defer cancel()
	round, err := c.client.Submit(submitCtx, deadline, prefix, challenge, signature, nodeID, *auth)
	switch {
	case err == nil:
		return round, nil
	case errors.Is(err, ErrUnauthorized):
		logger.Warn("failed to submit challenge as unathorized - recertifying", zap.Error(err))
		auth.PoetCert, err = c.recertify(ctx, nodeID)
		if err != nil {
			return nil, fmt.Errorf("recertifying: %w", err)
		}
		return c.client.Submit(submitCtx, deadline, prefix, challenge, signature, nodeID, *auth)
	}
	return nil, fmt.Errorf("submitting challenge: %w", err)
}

func (c *poetService) Proof(ctx context.Context, roundID string) (*types.PoetProof, []types.Hash32, error) {
	getProofsCtx, cancel := withConditionalTimeout(ctx, c.requestTimeout)
	defer cancel()

	c.gettingProof.Lock()
	defer c.gettingProof.Unlock()

	if members, ok := c.proofMembers[roundID]; ok {
		proof, err := c.db.ProofForRound(c.client.Id(), roundID)
		if err == nil {
			c.logger.Debug("returning cached proof", zap.String("round_id", roundID))
			return proof, members, nil
		}
		c.logger.Warn("cached members found but proof not found in db", zap.String("round_id", roundID), zap.Error(err))
	}

	proof, members, err := c.client.Proof(getProofsCtx, roundID)
	if err != nil {
		return nil, nil, fmt.Errorf("getting proof: %w", err)
	}

	if err := c.db.ValidateAndStore(ctx, proof); err != nil && !errors.Is(err, ErrObjectExists) {
		c.logger.Warn("failed to validate and store proof", zap.Error(err), zap.Object("proof", proof))
		return nil, nil, fmt.Errorf("validating and storing proof: %w", err)
	}
	clear(c.proofMembers)
	c.proofMembers[roundID] = members

	return &proof.PoetProof, members, nil
}

func (c *poetService) Certify(ctx context.Context, id types.NodeID) (*certifier.PoetCert, error) {
	if c.certifier == nil {
		return nil, errors.New("certifier not configured")
	}
	url, pubkey, err := c.client.CertifierInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting certifier info: %w", err)
	}
	return c.certifier.Certificate(ctx, id, url, pubkey)
}

func (c *poetService) recertify(ctx context.Context, id types.NodeID) (*certifier.PoetCert, error) {
	if c.certifier == nil {
		return nil, errors.New("certifier not configured")
	}
	url, pubkey, err := c.client.CertifierInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting certifier info: %w", err)
	}
	return c.certifier.Recertify(ctx, id, url, pubkey)
}
