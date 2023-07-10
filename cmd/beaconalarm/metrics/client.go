package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	beaconMetrics "github.com/spacemeshos/go-spacemesh/beacon/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

type Client struct {
	offset time.Duration

	client v1.API
	logger log.Logger
	clock  *timesync.NodeClock
}

func NewClient(opts ...ClientOptionFunc) (*Client, error) {
	options := &clientOption{
		logger:       log.NewNop(),
		roundTripper: http.DefaultTransport,
	}

	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, fmt.Errorf("failed to apply client option: %w", err)
		}
	}
	if err := options.validate(); err != nil {
		return nil, fmt.Errorf("invalid client options: %w", err)
	}

	// Create a custom HTTP client with authentication
	client, err := api.NewClient(api.Config{
		Address:      options.url,
		RoundTripper: options.roundTripper,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	offset := options.cfg.ProposalDuration + options.cfg.FirstVotingRoundDuration + time.Duration(options.cfg.RoundsNumber-1)*(options.cfg.VotingRoundDuration+options.cfg.WeakCoinRoundDuration)
	options.logger.With().Info("using alarm offset",
		log.Duration("offset", offset),
		log.Duration("proposalDuration", options.cfg.ProposalDuration),
		log.Duration("firstVotingRoundDuration", options.cfg.FirstVotingRoundDuration),
		log.Duration("votingRoundDuration", options.cfg.VotingRoundDuration),
		log.Duration("weakCoinRoundDuration", options.cfg.WeakCoinRoundDuration),
		log.FieldNamed("roundsNumber", options.cfg.RoundsNumber),
	)

	return &Client{
		offset: offset,
		client: v1.NewAPI(client),
		logger: options.logger,
		clock:  options.clock,
	}, nil
}

func (c *Client) FetchBeaconValue(ctx context.Context, namespace string, epoch types.EpochID) (string, error) {
	c.logger.With().Info("waiting for beacon value", log.FieldNamed("target_epoch", epoch))

	lid := types.EpochID(epoch - 1).FirstLayer()
	ts := c.clock.LayerToTime(lid)
	ts = ts.Add(c.offset).Add(30 * time.Second) // Add 30 seconds to account for the time it takes to fetch the metric from nodes

	c.logger.With().Info("waiting for beacon value", log.FieldNamed("target_epoch", epoch), log.Time("ts", ts))

	timer := time.NewTimer(time.Until(ts))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
	}

	c.logger.With().Info("fetching beacon value", log.FieldNamed("target_epoch", epoch), log.Time("ts", ts))
	result, warnings, err := c.client.Query(ctx, fmt.Sprintf(`group by(beacon) (%s{kubernetes_namespace="%s",epoch="%d"})`, beaconMetrics.MetricNameCalculatedWeight(), namespace, epoch), ts)
	if err != nil {
		return "", fmt.Errorf("failed to fetch metric: %w", err)
	}
	if len(warnings) > 0 {
		c.logger.With().Warning("query warnings:", log.Strings("warnings", warnings))
	}

	// Check if the result is a vector
	vector, ok := result.(model.Vector)
	if !ok {
		return "", fmt.Errorf("query result is not a vector")
	}

	if len(vector) != 1 {
		return "", fmt.Errorf("nodes did not find consensus on a single beacon value")
	}

	beaconValue := string(vector[0].Metric["beacon"])
	c.logger.With().Info("fetched beacon value", log.String("beacon", beaconValue))
	return beaconValue, nil
}
