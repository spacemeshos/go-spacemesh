package activation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/spacemeshos/poet/shared"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	certifierdb "github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

type CertifierClientConfig struct {
	// Base delay between retries, scaled with the number of retries.
	RetryDelay time.Duration `mapstructure:"retry-delay"`
	// Maximum time to wait between retries
	MaxRetryDelay time.Duration `mapstructure:"max-retry-delay"`
	// Maximum number of retries
	MaxRetries int `mapstructure:"max-retries"`
}

type CertifierConfig struct {
	Client CertifierClientConfig `mapstructure:"client"`
}

func DefaultCertifierClientConfig() CertifierClientConfig {
	return CertifierClientConfig{
		RetryDelay:    1 * time.Second,
		MaxRetryDelay: 30 * time.Second,
		MaxRetries:    5,
	}
}

func DefaultCertifierConfig() CertifierConfig {
	return CertifierConfig{
		Client: DefaultCertifierClientConfig(),
	}
}

type ProofToCertify struct {
	Nonce   uint32 `json:"nonce"`
	Indices []byte `json:"indices"`
	Pow     uint64 `json:"pow"`
}

type ProofToCertifyMetadata struct {
	NodeId          []byte `json:"node_id"`
	CommitmentAtxId []byte `json:"commitment_atx_id"`

	Challenge []byte `json:"challenge"`
	NumUnits  uint32 `json:"num_units"`
}

type CertifyRequest struct {
	Proof    ProofToCertify         `json:"proof"`
	Metadata ProofToCertifyMetadata `json:"metadata"`
}

type CertifyResponse struct {
	Certificate []byte `json:"certificate"`
	Signature   []byte `json:"signature"`
	PubKey      []byte `json:"pub_key"`
}

type Certifier struct {
	logger *zap.Logger
	db     *localsql.Database
	client certifierClient

	certificationsLock sync.Mutex
	certifications     map[string]chan struct{}
}

func NewCertifier(
	db *localsql.Database,
	logger *zap.Logger,
	client certifierClient,
) *Certifier {
	c := &Certifier{
		client:         client,
		logger:         logger,
		db:             db,
		certifications: make(map[string]chan struct{}),
	}

	return c
}

func (c *Certifier) Certificate(
	ctx context.Context,
	id types.NodeID,
	certifier *url.URL,
	pubkey []byte,
) (*certifierdb.PoetCert, error) {
	// We index certs in DB by node ID and pubkey. To avoid redundant queries, we allow only 1
	// request per (nodeID, pubkey) pair to be in flight at a time.
	key := string(append(id.Bytes(), pubkey...))
	c.certificationsLock.Lock()
	if ch, ok := c.certifications[key]; ok {
		c.certificationsLock.Unlock()
		<-ch
	} else {
		ch := make(chan struct{})
		c.certifications[key] = ch
		c.certificationsLock.Unlock()
		defer func() {
			c.certificationsLock.Lock()
			close(ch)
			delete(c.certifications, key)
			c.certificationsLock.Unlock()
		}()
	}

	cert, err := certifierdb.Certificate(c.db, id, pubkey)
	switch {
	case err == nil:
		return cert, nil
	case !errors.Is(err, sql.ErrNotFound):
		return nil, fmt.Errorf("getting certificate from DB for: %w", err)
	}
	return c.Recertify(ctx, id, certifier, pubkey)
}

func (c *Certifier) Recertify(
	ctx context.Context,
	id types.NodeID,
	certifier *url.URL,
	pubkey []byte,
) (*certifierdb.PoetCert, error) {
	cert, err := c.client.Certificate(ctx, id, certifier, pubkey)
	if err != nil {
		return nil, fmt.Errorf("certifying POST at %v: %w", certifier, err)
	}

	if err := certifierdb.AddCertificate(c.db, id, *cert, pubkey); err != nil {
		c.logger.Warn("failed to persist poet cert", zap.Error(err))
	}
	return cert, nil
}

type CertifierClient struct {
	client  *retryablehttp.Client
	logger  *zap.Logger
	db      sql.Executor
	localDb *localsql.Database
}

type certifierClientOpts func(*CertifierClient)

func WithCertifierClientConfig(cfg CertifierClientConfig) certifierClientOpts {
	return func(c *CertifierClient) {
		c.client.RetryMax = cfg.MaxRetries
		c.client.RetryWaitMin = cfg.RetryDelay
		c.client.RetryWaitMax = cfg.MaxRetryDelay
	}
}

func NewCertifierClient(
	db sql.Executor,
	localDb *localsql.Database,
	logger *zap.Logger,
	opts ...certifierClientOpts,
) *CertifierClient {
	c := &CertifierClient{
		client:  retryablehttp.NewClient(),
		logger:  logger,
		db:      db,
		localDb: localDb,
	}
	config := DefaultCertifierClientConfig()
	c.client.RetryMax = config.MaxRetries
	c.client.RetryWaitMin = config.RetryDelay
	c.client.RetryWaitMax = config.MaxRetryDelay
	c.client.Logger = &retryableHttpLogger{logger}
	c.client.ResponseLogHook = func(logger retryablehttp.Logger, resp *http.Response) {
		c.logger.Info("response received", zap.Stringer("url", resp.Request.URL), zap.Int("status", resp.StatusCode))
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *CertifierClient) obtainPostFromLastAtx(ctx context.Context, nodeId types.NodeID) (*nipost.Post, error) {
	atxid, err := atxs.GetLastIDByNodeID(c.db, nodeId)
	if err != nil {
		return nil, fmt.Errorf("no existing ATX found: %w", err)
	}
	atx, err := atxs.Get(c.db, atxid)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve ATX: %w", err)
	}
	atxNipost, err := loadNipost(ctx, c.db, atxid)
	if err != nil {
		return nil, errors.New("no NIPoST found in last ATX")
	}
	if atx.CommitmentATX == nil {
		if commitmentAtx, err := atxs.CommitmentATX(c.db, nodeId); err != nil {
			return nil, fmt.Errorf("failed to retrieve commitment ATX: %w", err)
		} else {
			atx.CommitmentATX = &commitmentAtx
		}
	}

	c.logger.Info("found POST in an existing ATX", zap.String("atx_id", atxid.Hash32().ShortString()))
	return &nipost.Post{
		Nonce:         atxNipost.Post.Nonce,
		Indices:       atxNipost.Post.Indices,
		Pow:           atxNipost.Post.Pow,
		Challenge:     atxNipost.PostMetadata.Challenge,
		NumUnits:      atx.NumUnits,
		CommitmentATX: *atx.CommitmentATX,
		// VRF nonce is not needed
	}, nil
}

func (c *CertifierClient) obtainPost(ctx context.Context, id types.NodeID) (*nipost.Post, error) {
	c.logger.Info("looking for POST for poet certification")
	post, err := nipost.GetPost(c.localDb, id)
	switch {
	case err == nil:
		c.logger.Info("found POST in local DB")
		return post, nil
	case errors.Is(err, sql.ErrNotFound):
		// no post found
	default:
		return nil, fmt.Errorf("loading initial post from db: %w", err)
	}

	c.logger.Info("POST not found in local DB. Trying to obtain POST from an existing ATX")
	if post, err := c.obtainPostFromLastAtx(ctx, id); err == nil {
		c.logger.Info("found POST in an existing ATX")
		if err := nipost.AddPost(c.localDb, id, *post); err != nil {
			c.logger.Error("failed to save post", zap.Error(err))
		}
		return post, nil
	}

	return nil, errors.New("PoST not found")
}

func (c *CertifierClient) Certificate(
	ctx context.Context,
	id types.NodeID,
	url *url.URL,
	pubkey []byte,
) (*certifierdb.PoetCert, error) {
	post, err := c.obtainPost(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("obtaining PoST: %w", err)
	}

	request := CertifyRequest{
		Proof: ProofToCertify{
			Pow:     post.Pow,
			Nonce:   post.Nonce,
			Indices: post.Indices,
		},
		Metadata: ProofToCertifyMetadata{
			NodeId:          id[:],
			CommitmentAtxId: post.CommitmentATX[:],
			NumUnits:        post.NumUnits,
			Challenge:       post.Challenge,
		},
	}

	jsonRequest, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, "POST", url.JoinPath("/certify").String(), jsonRequest)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body []byte
		if resp.Body != nil {
			body, _ = io.ReadAll(resp.Body)
		}
		return nil, fmt.Errorf("request failed with code %d (message: %s)", resp.StatusCode, body)
	}

	certResponse := CertifyResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&certResponse); err != nil {
		return nil, fmt.Errorf("decoding JSON response: %w", err)
	}
	if !bytes.Equal(certResponse.PubKey, pubkey) {
		return nil, errors.New("pubkey is invalid")
	}

	opaqueCert := &shared.OpaqueCert{
		Data:      certResponse.Certificate,
		Signature: certResponse.Signature,
	}

	cert, err := shared.VerifyCertificate(opaqueCert, pubkey, id.Bytes())
	if err != nil {
		return nil, fmt.Errorf("verifying certificate: %w", err)
	}

	if cert.Expiration != nil {
		c.logger.Info("certificate has expiration date", zap.Time("expiration", *cert.Expiration))
		if time.Until(*cert.Expiration) < 0 {
			return nil, errors.New("certificate is expired")
		}
	}

	return &certifierdb.PoetCert{
		Data:      opaqueCert.Data,
		Signature: opaqueCert.Signature,
	}, nil
}

// load NIPoST for the given ATX from the database.
func loadNipost(ctx context.Context, db sql.Executor, id types.ATXID) (*types.NIPost, error) {
	var blob sql.Blob
	version, err := atxs.LoadBlob(ctx, db, id.Bytes(), &blob)
	if err != nil {
		return nil, fmt.Errorf("getting blob for %s: %w", id, err)
	}

	switch version {
	case types.AtxV1:
		var atx wire.ActivationTxV1
		if err := codec.Decode(blob.Bytes, &atx); err != nil {
			return nil, fmt.Errorf("decoding ATX blob: %w", err)
		}
		return wire.NiPostFromWireV1(atx.NIPost), nil
	case types.AtxV2:
		// TODO: support ATX V2
	}
	panic("unsupported ATX version")
}
