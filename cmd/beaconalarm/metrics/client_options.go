package metrics

import (
	"errors"
	"net/http"

	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

type clientOption struct {
	url string
	cfg *beacon.Config

	logger       log.Logger
	clock        *timesync.NodeClock
	roundTripper http.RoundTripper
}

func (o *clientOption) validate() error {
	if o.url == "" {
		return errors.New("missing url")
	}
	if o.cfg == nil {
		return errors.New("missing beacon config")
	}
	if o.clock == nil {
		return errors.New("missing clock")
	}
	return nil
}

type ClientOptionFunc func(*clientOption) error

func WithURL(url string) ClientOptionFunc {
	return func(o *clientOption) error {
		o.url = url
		return nil
	}
}

func WithBeaconConfig(cfg beacon.Config) ClientOptionFunc {
	return func(o *clientOption) error {
		o.cfg = new(beacon.Config)
		*o.cfg = cfg
		return nil
	}
}

func WithLogger(logger log.Logger) ClientOptionFunc {
	return func(o *clientOption) error {
		o.logger = logger
		return nil
	}
}

func WithClock(clock *timesync.NodeClock) ClientOptionFunc {
	return func(o *clientOption) error {
		o.clock = clock
		return nil
	}
}

func WithBasicAuth(orgID, user, password string) ClientOptionFunc {
	return func(o *clientOption) error {
		o.roundTripper = RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.SetBasicAuth(user, password)
			req.Header.Add("X-Scope-OrgID", orgID)
			return http.DefaultTransport.RoundTrip(req)
		})
		return nil
	}
}

type RoundTripFunc func(req *http.Request) (*http.Response, error)

func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
