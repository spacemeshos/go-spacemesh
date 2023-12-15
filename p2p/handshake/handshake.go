package handshake

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/transport"
	"github.com/libp2p/go-msgio"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	handshakeTimeout       = 5 * time.Second
	handshakeAttempts      = 3
	handshakeRetryInterval = 1 * time.Second
	maxCookieSize          = 64
)

var cookieStreamPrefix = []byte{
	0x21, 0x23, 0x42, 0x42,
}

// NetworkCookie specifies a sequence of bytes that can be used to
// prevent peers from different networks from communicating with each
// other.
type NetworkCookie []byte

// NoNetworkCookie represents an empty NetworkCookie.
var NoNetworkCookie NetworkCookie = nil

// Empty returns true if the network cookie is empty.
func (nc NetworkCookie) Empty() bool {
	return len(nc) == 0
}

// String returns string representation of the NetworkCookie, which is
// a hex string.
func (nc NetworkCookie) String() string {
	return hex.EncodeToString(nc)
}

// Equal returns true if this cookie is the same as the other cookie.
func (nc NetworkCookie) Equal(other NetworkCookie) bool {
	return bytes.Equal(nc, other)
}

// Option specifies a handshake option.
type Option func(w *transportWrapper)

// WithLog specifies a logger for handshake.
func WithLog(logger log.Log) Option {
	return func(w *transportWrapper) {
		w.logger = logger
	}
}

// WithLog specifies handshake timeout.
func WithTimeout(t time.Duration) Option {
	return func(w *transportWrapper) {
		w.timeout = t
	}
}

// WithLog specifies handshake retry interval.
func WithRetryInterval(i time.Duration) Option {
	return func(w *transportWrapper) {
		w.retryInterval = i
	}
}

// WithAttempts specifies handshake retry count.
func WithAttempts(n int) Option {
	return func(w *transportWrapper) {
		w.attempts = n
	}
}

type transportWrapper struct {
	transport.Transport
	nc            NetworkCookie
	logger        log.Log
	timeout       time.Duration
	retryInterval time.Duration
	attempts      int
}

var (
	_ transport.Transport = &transportWrapper{}
	_ io.Closer           = &transportWrapper{}
)

// MaybeWrapTransport adds a handshake wrapper around the Transport if
// network cookie nc is not empty, otherwise it just returns the
// transport.
func MaybeWrapTransport(t transport.Transport, nc NetworkCookie, opts ...Option) transport.Transport {
	if nc.Empty() {
		return t
	}
	tr := &transportWrapper{
		Transport:     t,
		nc:            nc,
		logger:        log.NewNop(),
		timeout:       handshakeTimeout,
		retryInterval: handshakeRetryInterval,
		attempts:      handshakeAttempts,
	}
	for _, opt := range opts {
		opt(tr)
	}
	return tr
}

func (tr *transportWrapper) Dial(ctx context.Context, raddr ma.Multiaddr, p peer.ID) (transport.CapableConn, error) {
	c, err := tr.Transport.Dial(ctx, raddr, p)
	if err != nil {
		return nil, err
	}

	for i := 0; i < tr.attempts; i++ {
		var retry bool
		retry, err = tr.handshake(ctx, c)
		switch {
		case err == nil:
			return c, nil
		case retry:
			tr.logger.With().Error("transport wrapper handshake error",
				log.Int("attempt", i+1),
				log.Int("handshakeAttempts", tr.attempts),
				log.Err(err))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(handshakeRetryInterval):
			}
		default:
			return nil, err
		}
	}

	return nil, err
}

func (tr *transportWrapper) handshake(ctx context.Context, c transport.CapableConn) (retry bool, err error) {
	attemptCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	s, err := c.OpenStream(attemptCtx)
	if err != nil {
		return true, err
	}

	defer s.Close()
	s.SetDeadline(time.Now().Add(tr.timeout))

	if err := writeCookie(s, tr.nc); err != nil {
		return true, err
	}

	remoteCookie, mayHaveCookie, err := readCookie(s)
	if err != nil {
		return mayHaveCookie, err
	}

	if !tr.nc.Equal(remoteCookie) {
		return false, fmt.Errorf("cookie mismatch: %s instead of expected %s", remoteCookie, tr.nc)
	}

	return false, nil
}

func (tr *transportWrapper) Listen(laddr ma.Multiaddr) (transport.Listener, error) {
	l, err := tr.Transport.Listen(laddr)
	if err != nil {
		return nil, ma.ErrProtocolNotFound
	}
	return &listenerWrapper{
		Listener: l,
		nc:       tr.nc,
		logger:   tr.logger,
		timeout:  tr.timeout,
		attempts: tr.attempts,
	}, nil
}

func (tr *transportWrapper) Close() error {
	if c, ok := tr.Transport.(io.Closer); ok {
		return c.Close()
	}

	return nil
}

type listenerWrapper struct {
	transport.Listener
	nc       NetworkCookie
	logger   log.Log
	timeout  time.Duration
	attempts int
}

var _ transport.Listener = &listenerWrapper{}

func (l *listenerWrapper) Accept() (transport.CapableConn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		for i := 0; i < l.attempts; i++ {
			retry, err := l.handshake(c)
			switch {
			case err == nil:
				return c, nil
			case retry:
				l.logger.With().Error("transport wrapper handshake error",
					log.Int("attempt", i+1),
					log.Int("handshakeAttempts", l.attempts),
					log.Err(err))
			default:
				l.logger.With().Error("handshake failed", log.Err(err))
			}
		}
	}
}

func (l *listenerWrapper) handshake(c transport.CapableConn) (retry bool, err error) {
	s, err := c.AcceptStream()
	if err != nil {
		return true, err
	}
	defer s.Close()
	s.SetDeadline(time.Now().Add(l.timeout))

	remoteCookie, mayHaveCookie, err := readCookie(s)
	if err != nil {
		return mayHaveCookie, err
	}

	// Write our cookie even in case of mismatch so that the
	// peer sees the reason for disconnect immediately
	if err := writeCookie(s, l.nc); err != nil {
		return true, err
	}

	if !l.nc.Equal(remoteCookie) {
		return false, fmt.Errorf("cookie mismatch: %s instead of expected %s", remoteCookie, l.nc)
	}

	return false, nil
}

func writeCookie(w io.Writer, nc NetworkCookie) error {
	if _, err := w.Write(cookieStreamPrefix); err != nil {
		return err
	}
	return msgio.NewVarintWriter(w).WriteMsg(nc)
}

func readCookie(r io.Reader) (nc NetworkCookie, mayHaveCookie bool, err error) {
	buf := make([]byte, len(cookieStreamPrefix))
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, true, err
	}
	if !bytes.Equal(cookieStreamPrefix, buf) {
		return nil, false, fmt.Errorf("the remote endpoint doesn't have a cookie")
	}
	b, err := msgio.NewVarintReaderSize(r, maxCookieSize).ReadMsg()
	if err != nil {
		return nil, true, err
	}
	return NetworkCookie(b), true, err
}
