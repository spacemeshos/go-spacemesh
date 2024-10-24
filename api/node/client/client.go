package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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

func (s *NodeService) GetHareMessage(ctx context.Context, layer types.LayerID, round hare3.IterRound) ([]byte, error) {
	resp, err := s.client.GetHareRoundTemplateLayerIterRound(ctx, externalRef0.LayerID(layer), externalRef0.HareIter(round.Iter), externalRef0.HareRound(round.Round))
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

func (s *NodeService) TotalWeight(ctx context.Context, layer types.LayerID) (uint64, error) {
	resp, err := s.client.GetHareTotalWeightLayer(ctx, uint32(layer))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read all: %w", err)
	}
	return strconv.ParseUint(string(bytes), 10, 64)
}

func (s *NodeService) MinerWeight(ctx context.Context, layer types.LayerID, node types.NodeID) (uint64, error) {
	resp, err := s.client.GetHareWeightNodeIdLayer(ctx, node.String(), uint32(layer))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read all: %w", err)
	}
	return strconv.ParseUint(string(bytes), 10, 64)
}

func (s *NodeService) Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error) {
	v := types.Beacon{}
	resp, err := s.client.GetHareBeaconEpoch(ctx, externalRef0.EpochID(epoch))
	if err != nil {
		return v, err
	}
	if resp.StatusCode != http.StatusOK {
		return v, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return v, fmt.Errorf("read all: %w", err)
	}
	copy(v[:], bytes)
	return v, nil
}

func (s *NodeService) Proposal(ctx context.Context, layer types.LayerID, node types.NodeID) (*types.Proposal, uint64, error) {
	resp, err := s.client.GetProposalLayerNode(ctx, externalRef0.LayerID(layer), node.String())
	if err != nil {
		return nil, 0, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNoContent:
		// special case - no error but also no proposal, means
		// we're no eligibile this epoch with this node ID
		return nil, 0, nil
	default:
		return nil, 0, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read all: %w", err)
	}

	prop := types.Proposal{}
	codec.MustDecode(bytes, &prop)
	atxNonce := resp.Header.Get("x-spacemesh-atx-nonce")
	if atxNonce == "" {
		return nil, 0, errors.New("atx nonce header not found")
	}
	nonce, err := strconv.ParseUint(atxNonce, 10, 64)
	if err != nil {
		return nil, 0, fmt.Errorf("nonce parse: %w", err)
	}
	return &prop, nonce, nil
}
