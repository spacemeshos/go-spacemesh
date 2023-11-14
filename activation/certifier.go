package activation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/sourcegraph/conc/iter"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	certifier_db "github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
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

type Certificate struct {
	Poet        string    `mapstructure:"poet"`
	Certificate Base64Enc `mapstructure:"certificate"`
}

type CertifierConfig struct {
	Client       CertifierClientConfig `mapstructure:"client"`
	Certificates []Certificate         `mapstructure:"certificates"`
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

type ProoToCertifyfMetadata struct {
	NodeId          []byte `json:"node_id"`
	CommitmentAtxId []byte `json:"commitment_atx_id"`

	Challenge []byte `json:"challenge"`
	NumUnits  uint32 `json:"num_units"`
}

type CertifyRequest struct {
	Proof    ProofToCertify         `json:"proof"`
	Metadata ProoToCertifyfMetadata `json:"metadata"`
}

type CertifyResponse struct {
	Signature []byte `json:"signature"`
	PubKey    []byte `json:"pub_key"`
}

type Certifier struct {
	logger *zap.Logger
	db     *localsql.Database
	client certifierClient
}

type NewCertifierOption func(*Certifier)

func WithCertificates(certs []Certificate) NewCertifierOption {
	return func(c *Certifier) {
		c.logger.Info("adding certificates", zap.Int("num", len(certs)), zap.Any("certs", certs))
		for _, cert := range certs {
			if err := certifier_db.AddCertificate(c.db, c.client.Id(), cert.Certificate.Inner, cert.Poet); err != nil {
				c.logger.Warn("failed to persist poet cert", zap.Error(err))
			}
		}
	}
}

func NewCertifier(
	db *localsql.Database,
	logger *zap.Logger,
	client certifierClient,
	opts ...NewCertifierOption,
) *Certifier {
	c := &Certifier{
		client: client,
		logger: logger,
		db:     db,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *Certifier) GetCertificate(poet string) PoetCert {
	cert, err := certifier_db.Certificate(c.db, c.client.Id(), poet)
	switch {
	case err == nil:
		return cert
	case !errors.Is(err, sql.ErrNotFound):
		c.logger.Warn("failed to get certificate", zap.Error(err))
	}
	return nil
}

func (c *Certifier) Recertify(ctx context.Context, poet PoetClient) (PoetCert, error) {
	url, pubkey, err := poet.CertifierInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying certifier info: %w", err)
	}
	cert, err := c.client.Certify(ctx, url, pubkey)
	if err != nil {
		return nil, fmt.Errorf("certifying POST for %s at %v: %w", poet.Address(), url, err)
	}

	if err := certifier_db.AddCertificate(c.db, c.client.Id(), cert.Bytes(), poet.Address()); err != nil {
		c.logger.Warn("failed to persist poet cert", zap.Error(err))
	}

	return cert, nil
}

// CertifyAll certifies the nodeID for all poets that require a certificate.
// It optimizes the number of certification requests by taking a unique set of
// certifiers among the given poets and sending a single request to each of them.
// It returns a map of a poet address to a certificate for it.
func (c *Certifier) CertifyAll(ctx context.Context, poets []PoetClient) map[string]PoetCert {
	certs := make(map[string]PoetCert)
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

	certifierInfos := iter.Map(poetsToCertify, func(p *PoetClient) *certInfo {
		poet := *p
		url, pubkey, err := poet.CertifierInfo(ctx)
		if err != nil {
			c.logger.Warn("failed to query for certifier info", zap.Error(err), zap.String("poet", poet.Address()))
			return nil
		}
		return &certInfo{
			url:    url,
			pubkey: pubkey,
			poet:   poet.Address(),
		}
	})

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
			zap.Binary("cert", cert.Bytes()),
		)
		for _, poet := range svc.poets {
			if err := certifier_db.AddCertificate(c.db, c.client.Id(), cert.Bytes(), poet); err != nil {
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
		logger:   logger,
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

func (c *CertifierClient) Certify(ctx context.Context, url *url.URL, pubkey []byte) (PoetCert, error) {
	request := CertifyRequest{
		Proof: ProofToCertify{
			Pow:     c.post.Pow,
			Nonce:   c.post.Nonce,
			Indices: c.post.Indices,
		},
		Metadata: ProoToCertifyfMetadata{
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
	if !ed25519.Verify(pubkey, c.postInfo.NodeID[:], certRespose.Signature) {
		return nil, errors.New("signature is invalid")
	}
	return certRespose.Signature, nil
}
