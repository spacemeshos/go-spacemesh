package handshake

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/transport"
	quic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/quicreuse"
	ma "github.com/multiformats/go-multiaddr"
	quicgo "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func createPeer(t *testing.T) (peer.ID, crypto.PrivKey) {
	pk, _, err := crypto.GenerateECDSAKeyPair(rand.Reader)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(pk)
	require.NoError(t, err)
	return id, pk
}

func newConnManager(t *testing.T, opts ...quicreuse.Option) *quicreuse.ConnManager {
	t.Helper()
	cm, err := quicreuse.NewConnManager(quicgo.StatelessResetKey{}, quicgo.TokenGeneratorKey{}, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { cm.Close() })
	return cm
}

func wrapTransport(t transport.Transport, nc NetworkCookie, logger log.Log) transport.Transport {
	return MaybeWrapTransport(t, nc, WithLog(logger), WithTimeout(300*time.Millisecond), WithAttempts(2))
}

func TestTransportWrapper(t *testing.T) {
	type testcase struct {
		name         string
		clientCookie NetworkCookie
		serverCookie NetworkCookie
		dialError    bool
		readError    bool
	}

	testcases := []testcase{
		{
			name: "no cookies",
		},
		{
			name:         "matching cookies",
			clientCookie: NetworkCookie{0, 1, 2, 3},
			serverCookie: NetworkCookie{0, 1, 2, 3},
		},
		{
			name:         "non-matching cookies",
			clientCookie: NetworkCookie{0, 1, 2, 3},
			serverCookie: NetworkCookie{0, 1, 2, 0},
			dialError:    true,
		},
		{
			name:         "client cookie missing",
			serverCookie: NetworkCookie{0, 1, 2, 3},
			readError:    true,
		},
		{
			name:         "server cookie missing",
			clientCookie: NetworkCookie{0, 1, 2, 3},
			dialError:    true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			serverID, serverKey := createPeer(t)
			_, clientKey := createPeer(t)

			logger := logtest.New(t)

			serverTransport, err := quic.NewTransport(serverKey, newConnManager(t), nil, nil, nil)
			serverTransport = wrapTransport(serverTransport, tc.serverCookie, logger)
			require.NoError(t, err)
			defer serverTransport.(io.Closer).Close()

			ln, err := serverTransport.Listen(ma.StringCast("/ip4/127.0.0.1/udp/0/quic-v1"))
			require.NoError(t, err)
			var eg errgroup.Group
			eg.Go(func() error {
				c, err := ln.Accept()
				if err != nil {
					return fmt.Errorf("Accept error: %w", err)
				}
				t.Logf("accepted")
				s, err := c.AcceptStream()
				if err != nil {
					return fmt.Errorf("AcceptStream error: %w", err)
				}
				defer s.Close()
				buf := make([]byte, 4)
				_, err = io.ReadFull(s, buf)
				if err != nil {
					return fmt.Errorf("read error: %w", err)
				}
				exp := []byte{10, 20, 30, 40}
				if !bytes.Equal(buf, exp) {
					return fmt.Errorf("data mismatch: expected %s, got %s",
						hex.EncodeToString(exp), hex.EncodeToString(buf))
				}
				_, err = s.Write([]byte{11, 22, 33, 44})
				if err != nil {
					return fmt.Errorf("write error: %w", err)
				}
				return nil
			})

			defer eg.Wait()
			defer ln.Close()

			clientTransport, err := quic.NewTransport(clientKey, newConnManager(t), nil, nil, nil)
			clientTransport = wrapTransport(clientTransport, tc.clientCookie, logger)
			require.NoError(t, err)
			defer clientTransport.(io.Closer).Close()

			c, err := clientTransport.Dial(context.Background(), ln.Multiaddr(), serverID)
			if tc.dialError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				s, err := c.OpenStream(context.Background())
				require.NoError(t, err)
				defer s.Close()
				_, err = s.Write([]byte{10, 20, 30, 40})
				require.NoError(t, err)
				buf := make([]byte, 4)
				_, err = io.ReadFull(s, buf)
				if tc.readError {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.Equal(t, buf, []byte{11, 22, 33, 44})
					require.NoError(t, eg.Wait())
				}
			}
		})
	}
}
