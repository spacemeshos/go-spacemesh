package grpcserver

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
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

func genKeys(tb testing.TB) string {
	dir := tb.TempDir() // to store the generated files

	caKey, err := rsa.GenerateKey(rand.Reader, 4096)
	require.NoError(tb, err)
	caKeyBytes := x509.MarshalPKCS1PublicKey(&caKey.PublicKey)
	caKeyHash := sha1.Sum(caKeyBytes)

	f, err := os.Create(filepath.Join(dir, caKeyName))
	require.NoError(tb, err)
	defer f.Close()
	err = pem.Encode(f, &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caKey),
	})
	require.NoError(tb, err)

	snLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	snCA, err := rand.Int(rand.Reader, snLimit)
	require.NoError(tb, err)
	ca := &x509.Certificate{
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
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	require.NoError(tb, err)

	f, err = os.Create(filepath.Join(dir, caCertName))
	require.NoError(tb, err)
	defer f.Close()
	err = pem.Encode(f, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})
	require.NoError(tb, err)

	serverKey, err := rsa.GenerateKey(rand.Reader, 4096)
	require.NoError(tb, err)
	serverKeyBytes := x509.MarshalPKCS1PublicKey(&serverKey.PublicKey)
	serverKeyHash := sha1.Sum(serverKeyBytes)

	f, err = os.Create(filepath.Join(dir, serverKeyName))
	require.NoError(tb, err)
	defer f.Close()
	err = pem.Encode(f, &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(serverKey),
	})
	require.NoError(tb, err)

	snServer, err := rand.Int(rand.Reader, snLimit)
	require.NoError(tb, err)
	server := &x509.Certificate{
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
		IPAddresses:    []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:       []string{"localhost"},
		NotBefore:      time.Now(),
		NotAfter:       time.Now().AddDate(0, 1, 0),
	}
	serverCertBytes, err := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, caKey)
	require.NoError(tb, err)

	f, err = os.Create(filepath.Join(dir, serverCertName))
	require.NoError(tb, err)
	defer f.Close()
	err = pem.Encode(f, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: serverCertBytes,
	})
	require.NoError(tb, err)

	clientKey, err := rsa.GenerateKey(rand.Reader, 4096)
	require.NoError(tb, err)
	clientKeyBytes := x509.MarshalPKCS1PublicKey(&clientKey.PublicKey)
	clientKeyHash := sha1.Sum(clientKeyBytes)

	f, err = os.Create(filepath.Join(dir, clientKeyName))
	require.NoError(tb, err)
	defer f.Close()
	err = pem.Encode(f, &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(clientKey),
	})
	require.NoError(tb, err)

	snClient, err := rand.Int(rand.Reader, snLimit)
	require.NoError(tb, err)
	client := &x509.Certificate{
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
		IPAddresses:    []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:       []string{"localhost"},
		NotBefore:      time.Now(),
		NotAfter:       time.Now().AddDate(0, 1, 0),
	}
	clientCertBytes, err := x509.CreateCertificate(rand.Reader, client, ca, &clientKey.PublicKey, caKey)
	require.NoError(tb, err)

	f, err = os.Create(filepath.Join(dir, clientCertName))
	require.NoError(tb, err)
	defer f.Close()
	err = pem.Encode(f, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: clientCertBytes,
	})
	require.NoError(tb, err)
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
