package grpcserver

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	caCertName     = "ca.crt"
	caKeyName      = "ca.key"
	serverCertName = "server.crt"
	serverKeyName  = "server.key"
	clientCertName = "client.crt"
	clientKeyName  = "client.key"
)

func genPrivateKey(tb testing.TB, path string) *rsa.PrivateKey {
	caKey, err := rsa.GenerateKey(rand.Reader, 4096)
	require.NoError(tb, err)

	f, err := os.Create(path)
	require.NoError(tb, err)
	defer f.Close()
	require.NoError(tb, pem.Encode(f, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caKey),
	}))
	return caKey
}

func genCertificate(
	tb testing.TB,
	template,
	parent *x509.Certificate,
	pub *rsa.PublicKey,
	priv *rsa.PrivateKey,
	path string,
) {
	caBytes, err := x509.CreateCertificate(rand.Reader, template, parent, pub, priv)
	require.NoError(tb, err)

	f, err := os.Create(path)
	require.NoError(tb, err)
	defer f.Close()
	require.NoError(tb, pem.Encode(f, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	}))
}

func genKeys(tb testing.TB) string {
	dir := tb.TempDir() // to store the generated files

	snLimit := new(big.Int).Lsh(big.NewInt(1), 128) // limits how long the serial number of a key can be

	caKey := genPrivateKey(tb, filepath.Join(dir, caKeyName))
	caKeyBytes := x509.MarshalPKCS1PublicKey(&caKey.PublicKey)
	caKeyHash := sha256.Sum256(caKeyBytes)
	snCA, err := rand.Int(rand.Reader, snLimit)
	require.NoError(tb, err)

	caCert := &x509.Certificate{
		SerialNumber: snCA,
		Subject: pkix.Name{
			CommonName:   "ca.spacemesh.io",
			Organization: []string{"Spacemesh"},
			Country:      []string{"IL"},
			Province:     []string{"Spacemesh"},
			Locality:     []string{"Tel Aviv"},
		},
		SubjectKeyId:          caKeyHash[:],
		AuthorityKeyId:        caKeyHash[:],
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 1, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	genCertificate(tb, caCert, caCert, &caKey.PublicKey, caKey, filepath.Join(dir, caCertName))

	serverKey := genPrivateKey(tb, filepath.Join(dir, serverKeyName))
	serverKeyBytes := x509.MarshalPKCS1PublicKey(&serverKey.PublicKey)
	serverKeyHash := sha256.Sum256(serverKeyBytes)
	snServer, err := rand.Int(rand.Reader, snLimit)
	require.NoError(tb, err)

	serverCert := &x509.Certificate{
		SerialNumber: snServer,
		Subject: pkix.Name{
			CommonName:   "server.spacemesh.io",
			Organization: []string{"Server"},
			Country:      []string{"IL"},
			Province:     []string{"Spacemesh"},
			Locality:     []string{"Tel Aviv"},
		},
		SubjectKeyId:   serverKeyHash[:],
		AuthorityKeyId: caKeyHash[:],
		IPAddresses:    []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:       []string{"localhost"},
		NotBefore:      time.Now(),
		NotAfter:       time.Now().AddDate(0, 1, 0),
	}
	genCertificate(tb, serverCert, caCert, &serverKey.PublicKey, caKey, filepath.Join(dir, serverCertName))

	clientKey := genPrivateKey(tb, filepath.Join(dir, clientKeyName))
	clientKeyBytes := x509.MarshalPKCS1PublicKey(&clientKey.PublicKey)
	clientKeyHash := sha256.Sum256(clientKeyBytes)
	snClient, err := rand.Int(rand.Reader, snLimit)
	require.NoError(tb, err)

	clientCert := &x509.Certificate{
		SerialNumber: snClient,
		Subject: pkix.Name{
			CommonName:   "client.spacemesh.io",
			Organization: []string{"Client"},
			Country:      []string{"IL"},
			Province:     []string{"Spacemesh"},
			Locality:     []string{"Tel Aviv"},
		},
		SubjectKeyId:   clientKeyHash[:],
		AuthorityKeyId: caKeyHash[:],
		IPAddresses:    []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:       []string{"localhost"},
		NotBefore:      time.Now(),
		NotAfter:       time.Now().AddDate(0, 1, 0),
	}
	genCertificate(tb, clientCert, caCert, &clientKey.PublicKey, caKey, filepath.Join(dir, clientCertName))

	return dir
}

func launchTLSServer(tb testing.TB, certDir string, services ...ServiceAPI) (Config, func()) {
	caCert := filepath.Join(certDir, caCertName)
	serverCert := filepath.Join(certDir, serverCertName)
	serverKey := filepath.Join(certDir, serverKeyName)

	cfg := DefaultTestConfig()
	cfg.TLSListener = "127.0.0.1:0"
	cfg.TLSCACert = caCert
	cfg.TLSCert = serverCert
	cfg.TLSKey = serverKey

	grpcService, err := NewTLS(zaptest.NewLogger(tb).Named("grpc.TLS"), cfg, services)
	require.NoError(tb, err)

	// start gRPC server
	require.NoError(tb, grpcService.Start())

	// update config with bound addresses
	cfg.TLSListener = grpcService.BoundAddress

	return cfg, func() { assert.NoError(tb, grpcService.Close()) }
}
