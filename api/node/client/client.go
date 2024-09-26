package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/node/models"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

type nodeService struct {
	client *ClientWithResponses
	logger *zap.Logger
}

var (
	_ activation.AtxService = (*nodeService)(nil)
	_ pubsub.Publisher      = (*nodeService)(nil)
)

type Config struct {
	RetryWaitMin time.Duration // Minimum time to wait
	RetryWaitMax time.Duration // Maximum time to wait
	RetryMax     int           // Maximum number of retries
}

func NewNodeServiceClient(server string, logger *zap.Logger, cfg *Config) (*nodeService, error) {
	retryableClient := retryablehttp.Client{
		Logger:       &retryableHttpLogger{logger},
		RetryWaitMin: cfg.RetryWaitMin,
		RetryWaitMax: cfg.RetryWaitMax,
		RetryMax:     cfg.RetryMax,
		CheckRetry:   retryablehttp.DefaultRetryPolicy,
		Backoff:      retryablehttp.DefaultBackoff,
	}
	client, err := NewClientWithResponses(server, WithHTTPClient(retryableClient.StandardClient()))
	if err != nil {
		return nil, err
	}
	return &nodeService{
		client: client,
		logger: logger,
	}, nil
}

func (s *nodeService) Atx(ctx context.Context, id types.ATXID) (*types.ActivationTx, error) {
	resp, err := s.client.GetActivationAtxAtxIdWithResponse(ctx, hex.EncodeToString(id.Bytes()))
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode() {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, common.ErrNotFound
	default:
		return nil, fmt.Errorf("unexpected status: %s", resp.Status())
	}
	return models.ParseATX(resp.JSON200)
}

func (s *nodeService) LastATX(ctx context.Context, nodeID types.NodeID) (*types.ActivationTx, error) {
	resp, err := s.client.GetActivationLastAtxNodeIdWithResponse(ctx, hex.EncodeToString(nodeID.Bytes()))
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode() {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, common.ErrNotFound
	default:
		return nil, fmt.Errorf("unexpected status: %s", resp.Status())
	}
	return models.ParseATX(resp.JSON200)
}

func (s *nodeService) PositioningATX(ctx context.Context, maxPublish types.EpochID) (types.ATXID, error) {
	resp, err := s.client.GetActivationPositioningAtxPublishEpochWithResponse(ctx, maxPublish.Uint32())
	if err != nil {
		return types.ATXID{}, err
	}
	if resp.StatusCode() != http.StatusOK {
		return types.ATXID{}, fmt.Errorf("unexpected status: %s", resp.Status())
	}

	return models.ParseATXID(resp.JSON200.ID)
}

// Publish implements pubsub.Publisher.
func (s *nodeService) Publish(ctx context.Context, proto string, blob []byte) error {
	buf := bytes.NewBuffer(blob)
	protocol := PostPublishProtocolParamsProtocol(proto)
	resp, err := s.client.PostPublishProtocolWithBody(ctx, protocol, "application/octet-stream", buf)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}
	return nil
}
