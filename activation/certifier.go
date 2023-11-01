package activation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/natefinch/atomic"
	"github.com/sourcegraph/conc/iter"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/post/shared"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ProofToCertify struct {
	Nonce   uint32 `json:"nonce"`
	Indices []byte `json:"indices"`
	Pow     uint64 `json:"pow"`
}

type ProoToCertifyfMetadata struct {
	NodeId          []byte `json:"node_id"`
	CommitmentAtxId []byte `json:"commitment_atx_id"`

	Challenge     []byte `json:"challenge"`
	NumUnits      uint32 `json:"num_units"`
	LabelsPerUnit uint64 `json:"labels_per_unit"`
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
	client *retryablehttp.Client
	logger *zap.Logger
	store  *certificateStore

	post     *types.Post
	postInfo *types.PostInfo
}

func NewCertifier(datadir string, logger *zap.Logger, post *types.Post, postInfo *types.PostInfo) *Certifier {
	c := &Certifier{
		client:   retryablehttp.NewClient(),
		logger:   logger,
		store:    openCertificateStore(datadir, logger),
		post:     post,
		postInfo: postInfo,
	}
	c.client.Logger = &retryableHttpLogger{logger}
	c.client.ResponseLogHook = func(logger retryablehttp.Logger, resp *http.Response) {
		c.logger.Info("response received", zap.Stringer("url", resp.Request.URL), zap.Int("status", resp.StatusCode))
	}

	return c
}

func (c *Certifier) GetCertificate(poet string) *PoetCert {
	if cert, ok := c.store.get(poet); ok {
		return &cert
	}
	return nil
}

func (c *Certifier) Recertify(ctx context.Context, poet PoetClient) (*PoetCert, error) {
	info, err := poet.CertifierInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying certifier info: %w", err)
	}
	cert, err := c.certifyPost(ctx, info.URL, info.PubKey)
	if err != nil {
		return nil, fmt.Errorf("certifying POST for %s at %v: %w", poet.Address(), info.URL, info.PubKey)
	}
	c.store.put(poet.Address(), *cert)

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
			certs[poet.Address()] = *cert
		} else {
			poetsToCertify = append(poetsToCertify, poet)
		}
	}
	if len(poetsToCertify) == 0 {
		return certs
	}

	type certInfo struct {
		CertifierInfo
		poet string
	}

	certifierInfos := iter.Map(poetsToCertify, func(p *PoetClient) *certInfo {
		poet := *p
		info, err := poet.CertifierInfo(ctx)
		if err != nil {
			c.logger.Warn("failed to query for certifier info", zap.Error(err), zap.String("poet", poet.Address()))
			return nil
		}
		return &certInfo{
			CertifierInfo: *info,
			poet:          poet.Address(),
		}
	})

	type certService struct {
		CertifierInfo
		poets []string
	}
	certSvcs := make(map[string]*certService)
	for _, info := range certifierInfos {
		if info == nil {
			continue
		}

		if svc, ok := certSvcs[string(info.PubKey)]; !ok {
			certSvcs[string(info.PubKey)] = &certService{
				CertifierInfo: info.CertifierInfo,
				poets:         []string{info.poet},
			}
		} else {
			svc.poets = append(svc.poets, info.poet)
		}
	}

	for _, svc := range certSvcs {
		c.logger.Info(
			"certifying for poets",
			zap.Stringer("certifier", svc.URL),
			zap.Array("poets", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
				for _, poet := range svc.poets {
					enc.AppendString(poet)
				}
				return nil
			})),
		)

		cert, err := c.certifyPost(ctx, svc.URL, svc.PubKey)
		if err != nil {
			c.logger.Warn("failed to certify", zap.Error(err), zap.Stringer("certifier", svc.URL))
			continue
		}
		c.logger.Info(
			"sucessfully obtained certificate",
			zap.Stringer("certifier", svc.URL),
			zap.Binary("cert", cert.Signature),
		)
		for _, poet := range svc.poets {
			c.store.put(poet, *cert)
			certs[poet] = *cert
		}
	}
	c.store.persist()
	return certs
}

func (c *Certifier) certifyPost(ctx context.Context, url *url.URL, pubkey []byte) (*PoetCert, error) {
	request := CertifyRequest{
		Proof: ProofToCertify{
			Pow:     c.post.Pow,
			Nonce:   c.post.Nonce,
			Indices: c.post.Indices,
		},
		Metadata: ProoToCertifyfMetadata{
			NodeId:          c.postInfo.NodeID[:],
			CommitmentAtxId: c.postInfo.CommitmentATX[:],
			LabelsPerUnit:   c.postInfo.LabelsPerUnit,
			NumUnits:        c.postInfo.NumUnits,
			Challenge:       shared.ZeroChallenge,
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
	return &PoetCert{Signature: certRespose.Signature}, nil
}

type certificateStore struct {
	// protects certs map
	mu sync.RWMutex
	// map of certifier public key to PoET certificate
	certs map[string]PoetCert
	dir   string
}

func openCertificateStore(dir string, logger *zap.Logger) *certificateStore {
	store := &certificateStore{
		dir:   dir,
		certs: map[string]PoetCert{},
	}

	file, err := os.Open(filepath.Join(dir, "poet_certs.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return store
	case err != nil:
		logger.Warn("failed to open poet certs file", zap.Error(err))
		return store
	}
	certs := make(map[string]PoetCert)
	if err := json.NewDecoder(file).Decode(&certs); err != nil {
		logger.Warn("failed to decode poet certs", zap.Error(err))
		return store
	}
	return &certificateStore{
		certs: certs,
		dir:   dir,
	}
}

func (s *certificateStore) get(poet string) (PoetCert, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cert, ok := s.certs[poet]
	return cert, ok
}

func (s *certificateStore) put(poet string, cert PoetCert) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certs[poet] = cert
}

func (s *certificateStore) persist() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(s.certs); err != nil {
		return err
	}
	return atomic.WriteFile(filepath.Join(s.dir, "poet_certs.json"), buf)
}
