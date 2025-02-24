package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/node/models"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

type NodeService struct {
	client *ClientWithResponses
}

var (
	_ activation.Publisher  = (*NodeService)(nil)
	_ activation.AtxService = (*NodeService)(nil)
	_ pubsub.Publisher      = (*NodeService)(nil)
	_ hare3.NodeService     = (*NodeService)(nil)
)

type Config struct {
	RetryWaitMin time.Duration // Minimum time to wait
	RetryWaitMax time.Duration // Maximum time to wait
	RetryMax     int           // Maximum number of retries
}

func NewNodeServiceClient(server string, logger *zap.Logger, cfg *Config) (*NodeService, error) {
	if server == "" {
		return nil, errors.New("missing node-service server address")
	}
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
		return nil, activation.ErrNotFound
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
		return nil, activation.ErrNotFound
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

func (s *NodeService) PublishATX(ctx context.Context, blob []byte, poet *types.PoetProofMessage) error {
	body := PostActivationPublishJSONRequestBody{
		AtxBlob: blob,
	}
	if poet != nil {
		body.PoetProof = &models.PoetProof{
			Leafs: poet.LeafCount,
			Proof: models.MerkleProof{
				ProofNodes:   poet.ProofNodes,
				ProvenLeaves: poet.ProvenLeaves,
				Root:         poet.Root,
			},
			Statement: poet.Statement.Bytes(),
			Id:        poet.PoetServiceID,
			Round:     poet.RoundID,
		}
	}
	resp, err := s.client.PostActivationPublishWithResponse(ctx, body)
	if err != nil {
		return fmt.Errorf("failed request to publish ATX blob and poet: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to publish ATX and poet: %w (%s: %s)", err, resp.Status(), resp.Body)
	}
	return nil
}

// Publish implements pubsub.Publisher.
func (s *NodeService) Publish(ctx context.Context, proto string, blob []byte) error {
	buf := bytes.NewBuffer(blob)
	// The `hare3.DefaultProtocolName` contains slashes which
	// makes it unsuitable for a path parameter,
	// thus we change it to hare3 here and backwards on the server side.
	// FIXME: we check if `proto == ""` because the testnet config has
	// empty hare3 proto in `config/presets/testnet.go`.
	// Remove it after fixing the testnet preset.
	if proto == "" {
		proto = "testnet-hare3-workaround"
	}
	if proto == hare3.DefaultProtocolName {
		proto = "hare3"
	}
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

func (s *NodeService) HareRoundTemplate(
	ctx context.Context,
	layer types.LayerID,
	round hare3.IterRound,
) (*hare3.Body, error) {
	resp, err := s.client.GetHareRoundTemplateLayerIterRoundWithResponse(ctx,
		models.LayerID(layer),
		models.HareIter(round.Iter),
		models.HareRound(round.Round))
	if err != nil {
		return nil, fmt.Errorf("get hare message: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK:
		body := hare3.Body{
			Layer:     layer,
			IterRound: round,
		}
		for _, p := range resp.JSON200.Proposals {
			proposal, err := models.ParseHash20(p)
			if err != nil {
				return nil, fmt.Errorf("decoding proposal ID: %w", err)
			}
			body.Value.Proposals = append(body.Value.Proposals, types.ProposalID(proposal))
		}
		if refHex := resp.JSON200.Reference; refHex != nil {
			refHash, err := models.ParseHash32(*refHex)
			if err != nil {
				return nil, err
			}
			body.Value.Reference = &refHash
		}
		return &body, nil
	case http.StatusNoContent:
		// no message to return, special case, return nil,nil
		// and the caller should assume there's no message to process,
		// therefore hare probably terminated.
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected status: %q", resp.Status())
	}
}

func (s *NodeService) TotalWeight(ctx context.Context, epoch types.EpochID) (uint64, error) {
	resp, err := s.client.GetWeightsTotalEpochWithResponse(ctx, epoch.Uint32())
	if err != nil {
		return 0, fmt.Errorf("get total weight: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.JSON200.Weight, nil
	default:
		return 0, fmt.Errorf("unexpected status: %q", resp.Status())
	}
}

func (s *NodeService) MinerWeight(ctx context.Context, epoch types.EpochID, node types.NodeID) (uint64, error) {
	resp, err := s.client.GetWeightsMinerNodeIdEpochWithResponse(ctx, hex.EncodeToString(node.Bytes()), epoch.Uint32())
	if err != nil {
		return 0, fmt.Errorf("get miner weight: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.JSON200.Weight, nil
	default:
		return 0, fmt.Errorf("unexpected status: %q", resp.Status())
	}
}

func (s *NodeService) Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error) {
	resp, err := s.client.GetBeaconEpochWithResponse(ctx, epoch.Uint32())
	if err != nil {
		return types.Beacon{}, fmt.Errorf("get hare beacon: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK:
		beacon, err := models.ParseBeacon(resp.JSON200.Beacon)
		if err != nil {
			return types.Beacon{}, err
		}
		return beacon, nil
	default:
		return types.Beacon{}, fmt.Errorf("unexpected status: %q", resp.Status())
	}
}

func (s *NodeService) Proposal(ctx context.Context, layer types.LayerID, node types.NodeID) (
	*types.Proposal, uint64, error,
) {
	resp, err := s.client.GetProposalLayerNodeWithResponse(ctx, layer.Uint32(), hex.EncodeToString(node.Bytes()))
	if err != nil {
		return nil, 0, fmt.Errorf("get proposal layer: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK:
	case http.StatusNoContent:
		// special case - no error but also no proposal, means
		// we're not eligible this epoch with this node ID
		return nil, 0, nil
	default:
		return nil, 0, fmt.Errorf("unexpected status: %q", resp.Status())
	}

	atxID, err := models.ParseATXID(resp.JSON200.Ballot.AtxID)
	if err != nil {
		return nil, 0, err
	}
	opinionHash, err := models.ParseHash32(resp.JSON200.Ballot.OpinionHash)
	if err != nil {
		return nil, 0, err
	}
	meshHash, err := models.ParseHash32(resp.JSON200.MeshHash)
	if err != nil {
		return nil, 0, err
	}
	baseVote, err := models.ParseHash20(resp.JSON200.Ballot.Votes.Base)
	if err != nil {
		return nil, 0, err
	}

	support, err := models.ParseVotes(resp.JSON200.Ballot.Votes.Support)
	if err != nil {
		return nil, 0, fmt.Errorf("parsing support votes: %w", err)
	}
	against, err := models.ParseVotes(resp.JSON200.Ballot.Votes.Against)
	if err != nil {
		return nil, 0, fmt.Errorf("parsing against votes: %w", err)
	}
	txIds, err := models.ParseTransactionIDs(resp.JSON200.TxIDs)
	if err != nil {
		return nil, 0, err
	}

	prop := types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					Layer:       layer,
					AtxID:       atxID,
					OpinionHash: opinionHash,
				},
				SmesherID: node,
				Votes: types.Votes{
					Base:    types.BallotID(baseVote),
					Support: support,
					Against: against,
					Abstain: models.ParseLayers(resp.JSON200.Ballot.Votes.Abstain),
				},
			},
			TxIDs:    txIds,
			MeshHash: meshHash,
		},
	}
	if ref := resp.JSON200.Ballot.RefBallotID; ref != nil {
		ref, err := models.ParseHash20(*ref)
		if err != nil {
			return nil, 0, err
		}
		prop.RefBallot = types.BallotID(ref)
	} else {
		if resp.JSON200.Ballot.EpochData == nil {
			return nil, 0, errors.New("epoch data and refballot are both nil")
		}
		asHash, err := models.ParseHash32(resp.JSON200.Ballot.EpochData.ActiveSetHash)
		if err != nil {
			return nil, 0, err
		}
		beacon, err := models.ParseBeacon(resp.JSON200.Ballot.EpochData.Beacon)
		if err != nil {
			return nil, 0, err
		}
		prop.EpochData = &types.EpochData{
			ActiveSetHash:    asHash,
			Beacon:           beacon,
			EligibilityCount: resp.JSON200.Ballot.EpochData.EligibilityCount,
		}
	}
	return &prop, resp.JSON200.VrfNonce, nil
}

func (s *NodeService) CalculateEligibilitySlotsFor(
	ctx context.Context, node types.NodeID, epoch types.EpochID,
) (uint32, types.VRFPostIndex, error) {
	resp, err := s.client.GetEligibilitySlotsNodeEpochWithResponse(
		ctx,
		hex.EncodeToString(node.Bytes()),
		models.EpochID(epoch),
	)
	if err != nil {
		return 0, 0, err
	}
	switch resp.StatusCode() {
	case http.StatusOK:
		return resp.JSON200.Slots, types.VRFPostIndex(resp.JSON200.Nonce), nil
	default:
		return 0, 0, fmt.Errorf("unexpected status: %q", resp.Status())
	}
}
