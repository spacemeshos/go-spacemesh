package server

import (
	"bytes"
	"context"
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
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

//go:generate mockgen -typed -package=server -destination=mocks.go -source=server.go

type poetDB interface {
	ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error
}

type beaconService interface {
	Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error)
}

type hare interface {
	RoundTemplate(layer types.LayerID, round hare3.IterRound) *hare3.Body
}

type weights interface {
	TotalWeight(ctx context.Context, epoch types.EpochID) (uint64, error)
	MinerWeight(ctx context.Context, epoch types.EpochID, node types.NodeID) (uint64, error)
}

type proposalBuilder interface {
	BuildFor(ctx context.Context, layer types.LayerID, node types.NodeID) (*types.Proposal, types.VRFPostIndex, error)
	CalculateEligibilitySlotsFor(
		ctx context.Context, node types.NodeID, epoch types.EpochID) (uint32, types.VRFPostIndex, error)
}

type syncer interface {
	IsSynced(context.Context) bool
}

type Server struct {
	atxService activation.AtxService
	beacons    beaconService
	publisher  pubsub.Publisher
	poetDB     poetDB
	hare       hare
	weights    weights
	proposals  proposalBuilder
	logger     *zap.Logger
}

var _ StrictServerInterface = (*Server)(nil)

func NewServer(
	atxService activation.AtxService,
	beacons beaconService,
	publisher pubsub.Publisher,
	poetDB poetDB,
	hare hare,
	weights weights,
	proposals proposalBuilder,
	logger *zap.Logger,
) *Server {
	return &Server{
		atxService: atxService,
		beacons:    beacons,
		publisher:  publisher,
		poetDB:     poetDB,
		hare:       hare,
		weights:    weights,
		proposals:  proposals,
		logger:     logger,
	}
}

// IntoHandler turn the server into an HTTP handler.
// It will return '503 Unavailable' until the sync returns true for `IsSynced`.
func (s *Server) IntoHandler(mux *http.ServeMux, syncer syncer) http.Handler {
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
	readinessMid := func(f nethttp.StrictHTTPHandlerFunc, operationID string) nethttp.StrictHTTPHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			if syncer.IsSynced(ctx) {
				return f(ctx, w, r, req)
			} else {
				s.logger.Debug("received request while not synced",
					zap.String("path", r.URL.Path),
					zap.String("method", r.Method),
					zap.String("operation", operationID),
				)

				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("service not ready: not in sync with the network"))
				return nil, nil
			}
		}
	}

	return HandlerFromMux(NewStrictHandler(s, []StrictMiddlewareFunc{readinessMid, loggingMid}), mux)
}

// GetActivationAtxAtxId implements StrictServerInterface.
func (s *Server) GetActivationAtxAtxId(
	ctx context.Context,
	request GetActivationAtxAtxIdRequestObject,
) (GetActivationAtxAtxIdResponseObject, error) {
	id, err := models.ParseATXIDHex(request.AtxId)
	if err != nil {
		msg := err.Error()
		return GetActivationAtxAtxId400PlaintextResponse{
			Body:          bytes.NewBuffer([]byte(msg)),
			ContentLength: int64(len(msg)),
		}, nil
	}
	atx, err := s.atxService.Atx(ctx, id)
	switch {
	case errors.Is(err, activation.ErrNotFound):
		return GetActivationAtxAtxId404Response{}, nil
	case err != nil:
		return nil, err
	}

	return GetActivationAtxAtxId200JSONResponse{
		ID:           id.Bytes(),
		NumUnits:     atx.NumUnits,
		PublishEpoch: atx.PublishEpoch.Uint32(),
		Sequence:     &atx.Sequence,
		SmesherID:    atx.SmesherID.Bytes(),
		TickCount:    atx.TickCount,
		Weight:       atx.Weight,
	}, nil
}

// GetActivationLastAtxNodeId implements StrictServerInterface.
func (s *Server) GetActivationLastAtxNodeId(
	ctx context.Context,
	request GetActivationLastAtxNodeIdRequestObject,
) (GetActivationLastAtxNodeIdResponseObject, error) {
	id, err := models.ParseNodeIDHex(request.NodeId)
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
		ID:           atxid.ID().Bytes(),
		NumUnits:     atxid.NumUnits,
		PublishEpoch: atxid.PublishEpoch.Uint32(),
		Sequence:     &atxid.Sequence,
		SmesherID:    atxid.SmesherID.Bytes(),
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
		ID: id.Bytes(),
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
	// FIXME: we check if `proto == "testnet-hare3-workaround"` as a workaround for
	// an empty hare3 proto in `config/presets/testnet.go`.
	// The client set "testnet-hare3-workaround" and we change back to "".
	// Remove it after fixing the testnet preset.
	if protocol == "testnet-hare3-workaround" {
		protocol = ""
	}
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
		resp.Proposals = append(resp.Proposals, p[:])
	}
	if ref := body.Value.Reference; ref != nil {
		b := ref.Bytes()
		resp.Reference = &b
	}
	return resp, nil
}

func (s *Server) GetWeightsTotalEpoch(
	ctx context.Context,
	req GetWeightsTotalEpochRequestObject,
) (GetWeightsTotalEpochResponseObject, error) {
	weight, err := s.weights.TotalWeight(ctx, types.EpochID(req.Epoch))
	if err != nil {
		return nil, err
	}
	return &GetWeightsTotalEpoch200JSONResponse{Weight: weight}, nil
}

func (s *Server) GetWeightsMinerNodeIdEpoch(ctx context.Context,
	request GetWeightsMinerNodeIdEpochRequestObject,
) (GetWeightsMinerNodeIdEpochResponseObject, error) {
	id, err := models.ParseNodeIDHex(request.NodeId)
	if err != nil {
		msg := err.Error()
		return GetWeightsMinerNodeIdEpoch400PlaintextResponse{
			Body:          bytes.NewBuffer([]byte(msg)),
			ContentLength: int64(len(msg)),
		}, nil
	}
	weight, err := s.weights.MinerWeight(ctx, types.EpochID(request.Epoch), id)
	if err != nil {
		if errors.Is(err, eligibility.ErrNotActive) {
			return &GetWeightsMinerNodeIdEpoch200JSONResponse{Weight: 0}, nil
		}
		return nil, fmt.Errorf("miner weight: %w", err)
	}
	return &GetWeightsMinerNodeIdEpoch200JSONResponse{Weight: weight}, nil
}

func (s *Server) GetBeaconEpoch(
	ctx context.Context,
	request GetBeaconEpochRequestObject,
) (GetBeaconEpochResponseObject, error) {
	beacon, err := s.beacons.Beacon(ctx, types.EpochID(request.Epoch))
	if err != nil {
		return nil, err
	}
	return &GetBeaconEpoch200JSONResponse{Beacon: beacon[:]}, nil
}

func (s *Server) GetProposalLayerNode(ctx context.Context, request GetProposalLayerNodeRequestObject) (
	GetProposalLayerNodeResponseObject, error,
) {
	id, err := models.ParseNodeIDHex(request.Node)
	if err != nil {
		msg := err.Error()
		return GetProposalLayerNode400PlaintextResponse{
			Body:          bytes.NewBuffer([]byte(msg)),
			ContentLength: int64(len(msg)),
		}, nil
	}

	proposal, nonce, err := s.proposals.BuildFor(ctx, types.LayerID(request.Layer), id)
	if err != nil {
		return GetProposalLayerNode500Response{}, err
	}
	if proposal == nil {
		return GetProposalLayerNode204Response{}, nil
	}

	resp := GetProposalLayerNode200JSONResponse{
		Ballot: models.Ballot{
			AtxID:       proposal.AtxID.Bytes(),
			OpinionHash: proposal.OpinionHash.Bytes(),
			Votes: models.Votes{
				Abstain: encodeLayerIDs(proposal.Votes.Abstain),
				Against: encodeVotes(proposal.Votes.Against),
				Base:    proposal.Votes.Base[:],
				Support: encodeVotes(proposal.Votes.Support),
			},
		},
		TxIDs:    encodeSlicesOfBytes(proposal.TxIDs),
		VrfNonce: uint64(nonce),
		MeshHash: proposal.MeshHash.Bytes(),
	}

	if proposal.Ballot.RefBallot != types.EmptyBallotID {
		id := proposal.Ballot.RefBallot[:]
		resp.Ballot.RefBallotID = &id
	} else {
		if proposal.Ballot.EpochData.EligibilityCount == 0 {
			return GetProposalLayerNode204Response{}, nil
		}
		resp.Ballot.EpochData = &models.EpochData{
			ActiveSetHash:    proposal.EpochData.ActiveSetHash[:],
			Beacon:           proposal.EpochData.Beacon[:],
			EligibilityCount: proposal.EpochData.EligibilityCount,
		}
	}
	return resp, nil
}

type asBytes interface {
	Bytes() []byte
}

func encodeSlicesOfBytes[T asBytes](ids []T) []models.Bytes32 {
	encoded := make([]models.Bytes32, 0, len(ids))
	for _, h := range ids {
		encoded = append(encoded, h.Bytes())
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
			ID:      vote.ID[:],
			LayerID: vote.LayerID.Uint32(),
		})
	}
	return encoded
}

func (s *Server) GetEligibilitySlotsNodeEpoch(
	ctx context.Context,
	request GetEligibilitySlotsNodeEpochRequestObject,
) (GetEligibilitySlotsNodeEpochResponseObject, error) {
	id, err := models.ParseNodeIDHex(request.Node)
	if err != nil {
		msg := err.Error()
		return GetEligibilitySlotsNodeEpoch400PlaintextResponse{
			Body:          bytes.NewBuffer([]byte(msg)),
			ContentLength: int64(len(msg)),
		}, err
	}
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
