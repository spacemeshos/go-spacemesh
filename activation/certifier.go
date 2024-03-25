package activation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/spacemeshos/poet/shared"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
)

type CertifierClientConfig struct {
	// Base delay between retries, scaled with the number of retries.
	RetryDelay time.Duration `mapstructure:"retry-delay"`
	// Maximum time to wait between retries
	MaxRetryDelay time.Duration `mapstructure:"max-retry-delay"`
	// Maximum number of retries
	MaxRetries int `mapstructure:"max-retries"`
}

type Base64Enc struct {
	Inner []byte
}

func (e Base64Enc) String() string {
	return base64.RawStdEncoding.EncodeToString(e.Inner)
}

// Set implements pflag.Value.Set.
func (e *Base64Enc) Set(value string) error {
	return e.UnmarshalText([]byte(value))
}

// Type implements pflag.Value.Type.
func (Base64Enc) Type() string {
	return "Base64Enc"
}

func (e *Base64Enc) UnmarshalText(text []byte) error {
	b, err := base64.StdEncoding.DecodeString(string(text))
	if err != nil {
		return err
	}
	e.Inner = b
	return nil
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
}

func NewCertifier(
	db *localsql.Database,
	logger *zap.Logger,
	client certifierClient,
) *Certifier {
	c := &Certifier{
		client: client,
		logger: logger,
		db:     db,
	}

	return c
}

func (c *Certifier) GetCertificate(poet string) *certifier.PoetCert {
	cert, err := certifier.Certificate(c.db, c.client.Id(), poet)
	switch {
	case err == nil:
		return cert
	case !errors.Is(err, sql.ErrNotFound):
		c.logger.Warn("failed to get certificate", zap.Error(err))
	}
	return nil
}

func (c *Certifier) Recertify(ctx context.Context, poet PoetClient) (*certifier.PoetCert, error) {
	url, pubkey, err := poet.CertifierInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying certifier info: %w", err)
	}
	cert, err := c.client.Certify(ctx, url, pubkey)
	if err != nil {
		return nil, fmt.Errorf("certifying POST for %s at %v: %w", poet.Address(), url, err)
	}

	if err := certifier.AddCertificate(c.db, c.client.Id(), *cert, poet.Address()); err != nil {
		c.logger.Warn("failed to persist poet cert", zap.Error(err))
	}

	return cert, nil
}

// CertifyAll certifies the nodeID for all poets that require a certificate.
// It optimizes the number of certification requests by taking a unique set of
// certifiers among the given poets and sending a single request to each of them.
// It returns a map of a poet address to a certificate for it.
func (c *Certifier) CertifyAll(ctx context.Context, poets []PoetClient) map[string]*certifier.PoetCert {
	certs := make(map[string]*certifier.PoetCert)
	poetsToCertify := []PoetClient{}
	for _, poet := range poets {
		if cert := c.GetCertificate(poet.Address()); cert != nil {
			certs[poet.Address()] = cert
		} else {
			poetsToCertify = append(poetsToCertify, poet)
		}
	}
	if len(poetsToCertify) == 0 {
		return certs
	}

	type certInfo struct {
		url    *url.URL
		pubkey []byte
		poet   string
	}

	certifierInfos := make([]*certInfo, len(poetsToCertify))
	var eg errgroup.Group
	for i, poet := range poetsToCertify {
		i, poet := i, poet
		eg.Go(func() error {
			url, pubkey, err := poet.CertifierInfo(ctx)
			if err != nil {
				c.logger.Warn("failed to query for certifier info", zap.Error(err), zap.String("poet", poet.Address()))
				return nil
			}
			certifierInfos[i] = &certInfo{
				url:    url,
				pubkey: pubkey,
				poet:   poet.Address(),
			}
			return nil
		})
	}
	eg.Wait()

	type certService struct {
		url    *url.URL
		pubkey []byte
		poets  []string
	}
	certSvcs := make(map[string]*certService)
	for _, info := range certifierInfos {
		if info == nil {
			continue
		}

		if svc, ok := certSvcs[string(info.pubkey)]; !ok {
			certSvcs[string(info.pubkey)] = &certService{
				url:    info.url,
				pubkey: info.pubkey,
				poets:  []string{info.poet},
			}
		} else {
			svc.poets = append(svc.poets, info.poet)
		}
	}

	for _, svc := range certSvcs {
		c.logger.Info(
			"certifying for poets",
			zap.Stringer("certifier", svc.url),
			zap.Strings("poets", svc.poets),
		)

		cert, err := c.client.Certify(ctx, svc.url, svc.pubkey)
		if err != nil {
			c.logger.Warn("failed to certify", zap.Error(err), zap.Stringer("certifier", svc.url))
			continue
		}
		c.logger.Info(
			"successfully obtained certificate",
			zap.Stringer("certifier", svc.url),
			zap.Binary("cert data", cert.Data),
			zap.Binary("cert signature", cert.Signature),
		)
		for _, poet := range svc.poets {
			if err := certifier.AddCertificate(c.db, c.client.Id(), *cert, poet); err != nil {
				c.logger.Warn("failed to persist poet cert", zap.Error(err))
			}
			certs[poet] = cert
		}
	}
	return certs
}

type CertifierClient struct {
	client   *retryablehttp.Client
	post     *types.Post
	postInfo *types.PostInfo
	postCh   []byte
	logger   *zap.Logger
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
	logger *zap.Logger,
	post *types.Post,
	postInfo *types.PostInfo,
	postCh []byte,
	opts ...certifierClientOpts,
) *CertifierClient {
	c := &CertifierClient{
		client:   retryablehttp.NewClient(),
		logger:   logger.With(log.ZShortStringer("smesherID", postInfo.NodeID)),
		post:     post,
		postInfo: postInfo,
		postCh:   postCh,
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

func (c *CertifierClient) Id() types.NodeID {
	return c.postInfo.NodeID
}

func (c *CertifierClient) Certify(ctx context.Context, url *url.URL, pubkey []byte) (*certifier.PoetCert, error) {
	request := CertifyRequest{
		Proof: ProofToCertify{
			Pow:     c.post.Pow,
			Nonce:   c.post.Nonce,
			Indices: c.post.Indices,
		},
		Metadata: ProofToCertifyMetadata{
			NodeId:          c.postInfo.NodeID[:],
			CommitmentAtxId: c.postInfo.CommitmentATX[:],
			NumUnits:        c.postInfo.NumUnits,
			Challenge:       c.postCh,
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

	certRespose := CertifyResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&certRespose); err != nil {
		return nil, fmt.Errorf("decoding JSON response: %w", err)
	}
	if !bytes.Equal(certRespose.PubKey, pubkey) {
		return nil, errors.New("pubkey is invalid")
	}

	opaqueCert := &shared.OpaqueCert{
		Data:      certRespose.Certificate,
		Signature: certRespose.Signature,
	}

	cert, err := shared.VerifyCertificate(opaqueCert, pubkey, c.Id().Bytes())
	if err != nil {
		return nil, fmt.Errorf("verifying certificate: %w", err)
	}

	if cert.Expiration != nil {
		c.logger.Info("certificate has expiration date", zap.Time("expiration", *cert.Expiration))
		if time.Until(*cert.Expiration) < 0 {
			return nil, errors.New("certificate is expired")
		}
	}

	return &certifier.PoetCert{
		Data:      opaqueCert.Data,
		Signature: opaqueCert.Signature,
	}, nil
}
