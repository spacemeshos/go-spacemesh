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
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
)

//go:generate mockgen -typed -package=activation -destination=poet_mocks.go -source=./poet.go

var (
	ErrInvalidRequest           = errors.New("invalid request")
	ErrUnauthorized             = errors.New("unauthorized")
	ErrCertificatesNotSupported = errors.New("poet doesn't support certificates")
	ErrIncompatiblePhaseShift   = errors.New("fetched poet phase_shift is incompatible with configured phase_shift")
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
	CertifierInfo(ctx context.Context) (*types.CertifierInfo, error)
	Submit(
		ctx context.Context,
		deadline time.Time,
		prefix, challenge []byte,
		signature types.EdSignature,
		nodeID types.NodeID,
		auth PoetAuth,
	) (*types.PoetRound, error)
	Proof(ctx context.Context, roundID string) (*types.PoetProofMessage, []types.Hash32, error)
	Info(ctx context.Context) (*types.PoetInfo, error)
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
			c.logger.Debug(
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

func (c *HTTPPoetClient) CertifierInfo(ctx context.Context) (*types.CertifierInfo, error) {
	info, err := c.Info(ctx)
	if err != nil {
		return nil, err
	}
	if info.Certifier == nil {
		return nil, ErrCertificatesNotSupported
	}
	return info.Certifier, nil
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

func (c *HTTPPoetClient) Info(ctx context.Context) (*types.PoetInfo, error) {
	resBody := rpcapi.InfoResponse{}
	if err := c.req(ctx, http.MethodGet, "/v1/info", nil, &resBody); err != nil {
		return nil, fmt.Errorf("getting poet info: %w", err)
	}

	var certifierInfo *types.CertifierInfo
	if resBody.GetCertifier() != nil {
		url, err := url.Parse(resBody.GetCertifier().Url)
		if err != nil {
			return nil, fmt.Errorf("parsing certifier address: %w", err)
		}
		certifierInfo = &types.CertifierInfo{
			Url:    url,
			Pubkey: resBody.GetCertifier().Pubkey,
		}
	}

	return &types.PoetInfo{
		ServicePubkey: resBody.ServicePubkey,
		PhaseShift:    resBody.PhaseShift.AsDuration(),
		CycleGap:      resBody.CycleGap.AsDuration(),
		Certifier:     certifierInfo,
	}, nil
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
		c.logger.Debug("poet request failed", zap.String("status", res.Status), zap.String("body", string(data)))
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

type cachedData[T any] struct {
	mu   sync.Mutex
	data T
	exp  time.Time
	ttl  time.Duration
}

func (c *cachedData[T]) get(init func() (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.exp) {
		return c.data, nil
	}
	d, err := init()
	if err == nil {
		c.data = d
		c.exp = time.Now().Add(c.ttl)
	}
	return d, err
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

	certifierInfoCache cachedData[*types.CertifierInfo]
	mtx                *sync.Mutex
	expectedPhaseShift time.Duration
	fetchedPhaseShift  time.Duration
	powParamsCache     cachedData[*PoetPowParams]
}

type PoetServiceOpt func(*poetService)

func WithCertifier(certifier certifierService) PoetServiceOpt {
	return func(c *poetService) {
		c.certifier = certifier
	}
}

func NewPoetService(
	db poetDbAPI,
	server types.PoetServer,
	cfg PoetConfig,
	logger *zap.Logger,
	opts ...PoetServiceOpt,
) (*poetService, error) {
	client, err := NewHTTPPoetClient(server, cfg, WithLogger(logger))
	if err != nil {
		return nil, err
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
	opts ...PoetServiceOpt,
) *poetService {
	service := &poetService{
		db:                 db,
		logger:             logger,
		client:             client,
		requestTimeout:     cfg.RequestTimeout,
		certifierInfoCache: cachedData[*types.CertifierInfo]{ttl: cfg.CertifierInfoCacheTTL},
		powParamsCache:     cachedData[*PoetPowParams]{ttl: cfg.PowParamsCacheTTL},
		proofMembers:       make(map[string][]types.Hash32, 1),
		expectedPhaseShift: cfg.PhaseShift,
		mtx:                &sync.Mutex{},
	}
	for _, opt := range opts {
		opt(service)
	}

	err := service.verifyPhaseShiftConfiguration(context.Background())
	if err != nil {
		if errors.Is(err, ErrIncompatiblePhaseShift) {
			logger.Panic("failed to create poet service",
				zap.String("poet", client.Address()))
		}
		logger.Warn("failed to fetch poet phase shift",
			zap.Error(err),
			zap.String("poet", client.Address()))
	}
	return service
}

func (c *poetService) verifyPhaseShiftConfiguration(ctx context.Context) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.fetchedPhaseShift != 0 {
		return nil
	}
	resp, err := c.client.Info(ctx)
	if err != nil {
		return err
	} else if resp.PhaseShift != c.expectedPhaseShift {
		return ErrIncompatiblePhaseShift
	}

	c.fetchedPhaseShift = resp.PhaseShift
	return nil
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
	case errors.Is(err, ErrCertificatesNotSupported):
		logger.Debug("poet doesn't support certificates")
	default:
		logger.Warn("failed to certify", zap.Error(err))
	}
	// Fallback to PoW
	// TODO: remove this fallback once we migrate to certificates fully.
	logger.Info("falling back to PoW authorization")

	powCtx, cancel := withConditionalTimeout(ctx, c.requestTimeout)
	defer cancel()
	powParams, err := c.powParams(powCtx)
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

func (c *poetService) reauthorize(
	ctx context.Context,
	id types.NodeID,
	challenge []byte,
) (*PoetAuth, error) {
	if c.certifier != nil {
		if info, err := c.getCertifierInfo(ctx); err == nil {
			if err := c.certifier.DeleteCertificate(id, info.Pubkey); err != nil {
				return nil, fmt.Errorf("deleting cert: %w", err)
			}
		}
	}
	return c.authorize(ctx, id, challenge, c.logger)
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

	if err := c.verifyPhaseShiftConfiguration(ctx); err != nil {
		if errors.Is(err, ErrIncompatiblePhaseShift) {
			logger.Panic("failed to submit challenge",
				zap.String("poet", c.client.Address()))
		}
		return nil, err
	}

	// Try to obtain a certificate
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
		logger.Warn("failed to submit challenge as unauthorized - authorizing again", zap.Error(err))
		auth, err := c.reauthorize(ctx, nodeID, challenge)
		if err != nil {
			return nil, fmt.Errorf("authorizing: %w", err)
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

	if err := c.db.ValidateAndStore(ctx, proof); err != nil && !errors.Is(err, sql.ErrObjectExists) {
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
	info, err := c.getCertifierInfo(ctx)
	if err != nil {
		return nil, err
	}
	return c.certifier.Certificate(ctx, id, info.Url, info.Pubkey)
}

func (c *poetService) getCertifierInfo(ctx context.Context) (*types.CertifierInfo, error) {
	info, err := c.certifierInfoCache.get(func() (*types.CertifierInfo, error) {
		certifierInfo, err := c.client.CertifierInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting certifier info: %w", err)
		}
		return certifierInfo, nil
	})
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (c *poetService) powParams(ctx context.Context) (*PoetPowParams, error) {
	return c.powParamsCache.get(func() (*PoetPowParams, error) {
		return c.client.PowParams(ctx)
	})
}
