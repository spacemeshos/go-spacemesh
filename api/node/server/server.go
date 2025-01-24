package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
	"github.com/spacemeshos/poet/shared"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/node/models"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

//go:generate mockgen -typed -package=server -destination=mocks.go -source=server.go

type poetDB interface {
	ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error
}

type hare interface {
	RoundTemplate(layer types.LayerID, round hare3.IterRound) *hare3.Body
	TotalWeight(ctx context.Context, layer types.LayerID) (uint64, error)
	MinerWeight(ctx context.Context, node types.NodeID, layer types.LayerID) (uint64, error)
	Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error)
}

type proposalBuilder interface {
	BuildFor(ctx context.Context, layer types.LayerID, node types.NodeID) (*types.Proposal, types.VRFPostIndex, error)
	CalculateEligibilitySlotsFor(
		ctx context.Context, node types.NodeID, epoch types.EpochID) (uint32, types.VRFPostIndex, error)
}

type Server struct {
	atxService activation.AtxService
	publisher  pubsub.Publisher
	poetDB     poetDB
	hare       hare
	proposals  proposalBuilder
	logger     *zap.Logger
}

var _ StrictServerInterface = (*Server)(nil)

func NewServer(
	atxService activation.AtxService,
	publisher pubsub.Publisher,
	poetDB poetDB,
	hare hare,
	proposals proposalBuilder,
	logger *zap.Logger,
) *Server {
	return &Server{
		atxService: atxService,
		publisher:  publisher,
		poetDB:     poetDB,
		hare:       hare,
		proposals:  proposals,
		logger:     logger,
	}
}

func (s *Server) IntoHandler(mux *http.ServeMux) http.Handler {
	loggingMid := func(f nethttp.StrictHTTPHandlerFunc, operationID string) nethttp.StrictHTTPHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			uuid := uuid.New()
			s.logger.Debug(
				"request",
				zap.Stringer("request_id", uuid),
				zap.String("operation", operationID),
				zap.Any("request", req),
			)
			response, err := f(ctx, w, r, req)
			s.logger.Debug(
				"response",
				zap.Stringer("request_id", uuid),
				zap.String("operation", operationID),
				zap.Any("response", response),
				zap.Error(err),
			)
			return response, err
		}
	}
	return HandlerFromMux(NewStrictHandler(s, []StrictMiddlewareFunc{loggingMid}), mux)
}

func (s *Server) Start(address string) error {
	server := &http.Server{
		Handler: s.IntoHandler(http.NewServeMux()),
		Addr:    address,
	}
	return server.ListenAndServe()
}

// GetActivationAtxAtxId implements StrictServerInterface.
func (s *Server) GetActivationAtxAtxId(
	ctx context.Context,
	request GetActivationAtxAtxIdRequestObject,
) (GetActivationAtxAtxIdResponseObject, error) {
	idBytes, err := hex.DecodeString(request.AtxId)
	if err != nil {
		return nil, err
	}
	id := types.BytesToATXID(idBytes)
	atx, err := s.atxService.Atx(ctx, id)
	switch {
	case errors.Is(err, activation.ErrNotFound):
		return GetActivationAtxAtxId404Response{}, nil
	case err != nil:
		return nil, err
	}

	return GetActivationAtxAtxId200JSONResponse{
		ID:           request.AtxId,
		NumUnits:     atx.NumUnits,
		PublishEpoch: atx.PublishEpoch.Uint32(),
		Sequence:     &atx.Sequence,
		SmesherID:    hex.EncodeToString(atx.SmesherID.Bytes()),
		TickCount:    atx.TickCount,
		Weight:       atx.Weight,
	}, nil
}

// GetActivationLastAtxNodeId implements StrictServerInterface.
func (s *Server) GetActivationLastAtxNodeId(
	ctx context.Context,
	request GetActivationLastAtxNodeIdRequestObject,
) (GetActivationLastAtxNodeIdResponseObject, error) {
	id, err := models.ParseNodeID(request.NodeId)
	if err != nil {
		msg := err.Error()
		return GetActivationLastAtxNodeId400PlaintextResponse{
			Body:          bytes.NewBuffer([]byte(msg)),
			ContentLength: int64(len(msg)),
		}, nil
	}

	atxid, err := s.atxService.LastATX(ctx, id)
	switch {
	case errors.Is(err, activation.ErrNotFound):
		return GetActivationLastAtxNodeId404Response{}, nil
	case err != nil:
		return nil, err
	}

	return GetActivationLastAtxNodeId200JSONResponse{
		ID:           hex.EncodeToString(atxid.ID().Bytes()),
		NumUnits:     atxid.NumUnits,
		PublishEpoch: atxid.PublishEpoch.Uint32(),
		Sequence:     &atxid.Sequence,
		SmesherID:    hex.EncodeToString(atxid.SmesherID.Bytes()),
		TickCount:    atxid.TickCount,
		Weight:       atxid.Weight,
	}, nil
}

// GetActivationPositioningAtxEpoch implements StrictServerInterface.
func (s *Server) GetActivationPositioningAtxPublishEpoch(
	ctx context.Context,
	request GetActivationPositioningAtxPublishEpochRequestObject,
) (GetActivationPositioningAtxPublishEpochResponseObject, error) {
	id, err := s.atxService.PositioningATX(ctx, types.EpochID(request.PublishEpoch))
	if err != nil {
		return nil, err
	}

	return GetActivationPositioningAtxPublishEpoch200JSONResponse{
		ID: hex.EncodeToString(id.Bytes()),
	}, nil
}

func (s *Server) PostActivationPublish(
	ctx context.Context,
	request PostActivationPublishRequestObject,
) (PostActivationPublishResponseObject, error) {
	invalidArg := func(err error) (PostActivationPublish400PlaintextResponse, error) {
		msg := err.Error()
		return PostActivationPublish400PlaintextResponse{
			Body:          bytes.NewBuffer([]byte(msg)),
			ContentLength: int64(len(msg)),
		}, nil
	}

	if p := request.Body.PoetProof; p != nil {
		stmt, err := models.ParseHash32(p.Statement)
		if err != nil {
			return invalidArg(err)
		}
		proof := types.PoetProofMessage{
			PoetProof: types.PoetProof{
				MerkleProof: shared.MerkleProof{
					Root:         p.Proof.Root,
					ProvenLeaves: p.Proof.ProvenLeaves,
					ProofNodes:   p.Proof.ProofNodes,
				},
				LeafCount: p.Leafs,
			},
			Statement:     stmt,
			PoetServiceID: p.Id,
			RoundID:       p.Round,
		}
		if err := s.poetDB.ValidateAndStore(ctx, &proof); err != nil {
			return invalidArg(err)
		}
	}
	if err := s.publisher.Publish(ctx, pubsub.AtxProtocol, request.Body.AtxBlob); err != nil {
		return invalidArg(err)
	}

	return PostActivationPublish200Response{}, nil
}

// PostPublishProtocol implements StrictServerInterface.
func (s *Server) PostPublishProtocol(
	ctx context.Context,
	request PostPublishProtocolRequestObject,
) (PostPublishProtocolResponseObject, error) {
	blob, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	protocol := string(request.Protocol)
	if protocol == "hare3" {
		// Revert protocol change (avoiding slashes) done on the client side.
		// TODO: hare3 takes that from configuration what also should be done
		// there instead of using the default value
		protocol = hare3.DefaultProtocolName
	}
	s.publisher.Publish(ctx, protocol, blob)
	return PostPublishProtocol200Response{}, nil
}

func (s *Server) GetHareRoundTemplateLayerIterRound(ctx context.Context,
	request GetHareRoundTemplateLayerIterRoundRequestObject,
) (GetHareRoundTemplateLayerIterRoundResponseObject, error) {
	body := s.hare.RoundTemplate(types.LayerID(request.Layer),
		hare3.IterRound{
			Round: hare3.Round(request.Round),
			Iter:  (request.Iter),
		})
	if body == nil {
		return GetHareRoundTemplateLayerIterRound204Response{}, nil
	}

	var resp GetHareRoundTemplateLayerIterRound200JSONResponse
	for _, p := range body.Value.Proposals {
		resp.Proposals = append(resp.Proposals, hex.EncodeToString(p[:]))
	}
	if ref := body.Value.Reference; ref != nil {
		refHex := hex.EncodeToString(ref[:])
		resp.Reference = &refHex
	}
	return resp, nil
}

func (s *Server) GetHareTotalWeightLayer(ctx context.Context,
	req GetHareTotalWeightLayerRequestObject,
) (GetHareTotalWeightLayerResponseObject, error) {
	weight, err := s.hare.TotalWeight(ctx, types.LayerID(req.Layer))
	if err != nil {
		return nil, err
	}
	return &GetHareTotalWeightLayer200JSONResponse{Weight: weight}, nil
}

func (s *Server) GetHareWeightNodeIdLayer(ctx context.Context,
	request GetHareWeightNodeIdLayerRequestObject,
) (GetHareWeightNodeIdLayerResponseObject, error) {
	hexBuf, err := hex.DecodeString(request.NodeId)
	if err != nil {
		return nil, fmt.Errorf("decode node id: %w", err)
	}
	id := types.BytesToNodeID(hexBuf)
	weight, err := s.hare.MinerWeight(ctx, id, types.LayerID(request.Layer))
	if err != nil {
		return nil, fmt.Errorf("miner weight: %w", err)
	}
	return &GetHareWeightNodeIdLayer200JSONResponse{Weight: weight}, nil
}

func (s *Server) GetHareBeaconEpoch(ctx context.Context,
	request GetHareBeaconEpochRequestObject,
) (GetHareBeaconEpochResponseObject, error) {
	beacon, err := s.hare.Beacon(ctx, types.EpochID(request.Epoch))
	if err != nil {
		return nil, err
	}
	return &GetHareBeaconEpoch200JSONResponse{Beacon: hex.EncodeToString(beacon[:])}, nil
}

func (s *Server) GetProposalLayerNode(ctx context.Context, request GetProposalLayerNodeRequestObject) (
	GetProposalLayerNodeResponseObject, error,
) {
	hexBuf, err := hex.DecodeString(request.Node)
	if err != nil {
		return GetProposalLayerNode500Response{}, err
	}
	id := types.BytesToNodeID(hexBuf)

	proposal, nonce, err := s.proposals.BuildFor(ctx, types.LayerID(request.Layer), id)
	if err != nil {
		return GetProposalLayerNode500Response{}, err
	}
	if proposal == nil {
		return GetProposalLayerNode204Response{}, nil
	}

	resp := GetProposalLayerNode200JSONResponse{
		Ballot: models.Ballot{
			AtxID:       hex.EncodeToString(proposal.AtxID.Bytes()),
			OpinionHash: hex.EncodeToString(proposal.OpinionHash.Bytes()),
			Votes: models.Votes{
				Abstain: encodeLayerIDs(proposal.Votes.Abstain),
				Against: encodeVotes(proposal.Votes.Against),
				Base:    hex.EncodeToString(proposal.Votes.Base[:]),
				Support: encodeVotes(proposal.Votes.Support),
			},
		},
		TxIDs:    encodeSlicesOfBytes(proposal.TxIDs),
		VrfNonce: uint64(nonce),
		MeshHash: hex.EncodeToString(proposal.MeshHash.Bytes()),
	}

	if proposal.Ballot.RefBallot != types.EmptyBallotID {
		refBallotIDHex := hex.EncodeToString(proposal.RefBallot[:])
		resp.Ballot.RefBallotID = &refBallotIDHex
	} else {
		if proposal.Ballot.EpochData.EligibilityCount == 0 {
			return GetProposalLayerNode204Response{}, nil
		}
		resp.Ballot.EpochData = &models.EpochData{
			ActiveSetHash:    hex.EncodeToString(proposal.EpochData.ActiveSetHash[:]),
			Beacon:           hex.EncodeToString(proposal.EpochData.Beacon[:]),
			EligibilityCount: proposal.EpochData.EligibilityCount,
		}
	}
	return resp, nil
}

type asBytes interface {
	Bytes() []byte
}

func encodeSlicesOfBytes[T asBytes](ids []T) []models.Hash32 {
	encoded := make([]models.Hash32, 0, len(ids))
	for _, h := range ids {
		encoded = append(encoded, hex.EncodeToString(h.Bytes()))
	}
	return encoded
}

func encodeLayerIDs(lids []types.LayerID) []models.LayerID {
	encoded := make([]models.LayerID, 0, len(lids))
	for _, l := range lids {
		encoded = append(encoded, l.Uint32())
	}
	return encoded
}

func encodeVotes(votes []types.Vote) []models.Vote {
	encoded := make([]models.Vote, 0, len(votes))
	for _, vote := range votes {
		encoded = append(encoded, models.Vote{
			Height:  vote.Height,
			ID:      hex.EncodeToString(vote.ID[:]),
			LayerID: vote.LayerID.Uint32(),
		})
	}
	return encoded
}

func (s *Server) GetEligibilitySlotsNodeEpoch(
	ctx context.Context,
	request GetEligibilitySlotsNodeEpochRequestObject,
) (GetEligibilitySlotsNodeEpochResponseObject, error) {
	hexBuf, err := hex.DecodeString(request.Node)
	if err != nil {
		return GetEligibilitySlotsNodeEpoch200JSONResponse{}, err
	}
	id := types.BytesToNodeID(hexBuf)
	epoch := types.EpochID(request.Epoch)

	slots, nonce, err := s.proposals.CalculateEligibilitySlotsFor(ctx, id, epoch)
	if err != nil {
		return GetEligibilitySlotsNodeEpoch200JSONResponse{}, err
	}

	return GetEligibilitySlotsNodeEpoch200JSONResponse{
		Slots: slots,
		Nonce: uint64(nonce),
	}, nil
}
