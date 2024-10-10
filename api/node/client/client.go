package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/node/models"
	externalRef0 "github.com/spacemeshos/go-spacemesh/api/node/models"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

type NodeService struct {
	client *ClientWithResponses
	logger *zap.Logger
}

var (
	_ activation.AtxService   = (*NodeService)(nil)
	_ activation.PoetDbStorer = (*NodeService)(nil)
	_ pubsub.Publisher        = (*NodeService)(nil)
)

type Config struct {
	RetryWaitMin time.Duration // Minimum time to wait
	RetryWaitMax time.Duration // Maximum time to wait
	RetryMax     int           // Maximum number of retries
}

func NewNodeServiceClient(server string, logger *zap.Logger, cfg *Config) (*NodeService, error) {
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
	return &NodeService{
		client: client,
		logger: logger,
	}, nil
}

func (s *NodeService) Atx(ctx context.Context, id types.ATXID) (*types.ActivationTx, error) {
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

func (s *NodeService) LastATX(ctx context.Context, nodeID types.NodeID) (*types.ActivationTx, error) {
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

func (s *NodeService) PositioningATX(ctx context.Context, maxPublish types.EpochID) (types.ATXID, error) {
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
func (s *NodeService) Publish(ctx context.Context, proto string, blob []byte) error {
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

// StorePoetProof implements activation.PoetDbStorer.
func (s *NodeService) StorePoetProof(ctx context.Context, proof *types.PoetProofMessage) error {
	blob := codec.MustEncode(proof)
	buf := bytes.NewBuffer(blob)
	resp, err := s.client.PostPoetWithBody(ctx, "application/octet-stream", buf)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}
	return nil
}

func (s *NodeService) GetHareMessage(ctx context.Context, layer types.LayerID, round hare3.Round) ([]byte, error) {
	resp, err := s.client.GetHareRoundTemplateLayerRoundWithBody(ctx, externalRef0.LayerID(layer), externalRef0.HareRound(round), "application/octet-stream", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read all: %w", err)
	}
	return bytes, nil
}

func (s *NodeService) PublishHareMessage(ctx context.Context, msg []byte) error {
	buf := bytes.NewBuffer(msg)
	resp, err := s.client.PostHarePublishWithBody(ctx, "application/octet-stream", buf)
	if err != nil {
		return fmt.Errorf("publish hare: %w", err)
	}

	switch resp.StatusCode {
	case 202:
		return nil
	case 500:
		return errors.New("error processing send")
	default:
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}
