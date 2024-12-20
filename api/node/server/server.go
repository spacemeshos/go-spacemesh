package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/node/models"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

//go:generate mockgen -typed -package=server -destination=mocks.go -source=server.go

type poetDB interface {
	ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error
}

type hare interface {
	RoundMessage(layer types.LayerID, round hare3.IterRound) *hare3.Message
	TotalWeight(ctx context.Context, layer types.LayerID) (uint64, error)
	MinerWeight(ctx context.Context, node types.NodeID, layer types.LayerID) (uint64, error)
	Beacon(ctx context.Context, epoch types.EpochID) (types.Beacon, error)
}

type proposalBuilder interface {
	BuildFor(ctx context.Context, layer types.LayerID, node types.NodeID) (*types.Proposal, types.VRFPostIndex, error)
	CalculateEligibilitySlotsFor(ctx context.Context, node types.NodeID, epoch types.EpochID) (uint32, types.VRFPostIndex, error)
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
		// TODO: hare3 takes that from configuration what also should be done
		// there instead of using the default value
		protocol = hare3.DefaultProtocolName
	}
	s.publisher.Publish(ctx, protocol, blob)
	return PostPublishProtocol200Response{}, nil
}

// PostPoet implements StrictServerInterface.
func (s *Server) PostPoet(ctx context.Context, request PostPoetRequestObject) (PostPoetResponseObject, error) {
	var proof types.PoetProofMessage
	blob, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	if err := codec.Decode(blob, &proof); err != nil {
		msg := err.Error()
		return PostPoet400PlaintextResponse{
			Body:          bytes.NewBuffer([]byte(msg)),
			ContentLength: int64(len(msg)),
		}, nil
	}

	if err := s.poetDB.ValidateAndStore(ctx, &proof); err != nil {
		msg := err.Error()
		return PostPoet400PlaintextResponse{
			Body:          bytes.NewBuffer([]byte(msg)),
			ContentLength: int64(len(msg)),
		}, nil
	}
	return PostPoet200Response{}, nil
}

type hareResponse struct {
	message []byte
}

func (h *hareResponse) VisitGetHareRoundTemplateLayerIterRoundResponse(w http.ResponseWriter) error {
	if h.message == nil {
		w.WriteHeader(204) // no content
		return nil
	}
	w.Header().Add("content-type", "application/octet-stream")
	w.WriteHeader(200)
	_, err := w.Write(h.message)
	return err
}

func (s *Server) GetHareRoundTemplateLayerIterRound(ctx context.Context,
	request GetHareRoundTemplateLayerIterRoundRequestObject,
) (GetHareRoundTemplateLayerIterRoundResponseObject, error) {
	msg := s.hare.RoundMessage(types.LayerID(request.Layer),
		hare3.IterRound{
			Round: hare3.Round(request.Round),
			Iter:  (request.Iter),
		})
	if msg == nil {
		return &hareResponse{}, nil
	}

	return &hareResponse{
		message: codec.MustEncode(msg),
	}, nil
}

type totalWeightResp struct {
	w uint64
}

func (t *totalWeightResp) VisitGetHareTotalWeightLayerResponse(w http.ResponseWriter) error {
	w.Header().Add("content-type", "application/octet-stream")
	w.WriteHeader(200)
	_, err := w.Write([]byte(strconv.FormatUint(t.w, 10)))
	return err
}

func (s *Server) GetHareTotalWeightLayer(ctx context.Context,
	req GetHareTotalWeightLayerRequestObject,
) (GetHareTotalWeightLayerResponseObject, error) {
	weight, err := s.hare.TotalWeight(ctx, types.LayerID(req.Layer))
	if err != nil {
		return nil, err
	}
	return &totalWeightResp{weight}, nil
}

type nodeWeightResp struct {
	val uint64
}

func (n *nodeWeightResp) VisitGetHareWeightNodeIdLayerResponse(w http.ResponseWriter) error {
	w.Header().Add("content-type", "application/octet-stream")
	w.WriteHeader(200)
	_, err := w.Write([]byte(strconv.FormatUint(n.val, 10)))
	return err
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
	return &nodeWeightResp{val: weight}, nil
}

type beaconResp struct{ b types.Beacon }

func (b *beaconResp) VisitGetHareBeaconEpochResponse(w http.ResponseWriter) error {
	w.Header().Add("content-type", "application/octet-stream")
	w.WriteHeader(200)
	_, err := w.Write(b.b[:])
	return err
}

func (s *Server) GetHareBeaconEpoch(ctx context.Context,
	request GetHareBeaconEpochRequestObject,
) (GetHareBeaconEpochResponseObject, error) {
	beacon, err := s.hare.Beacon(ctx, types.EpochID(request.Epoch))
	if err != nil {
		return nil, err
	}
	return &beaconResp{b: beacon}, nil
}

type proposalResp struct {
	buf   []byte
	nonce types.VRFPostIndex
}

func (p *proposalResp) VisitGetProposalLayerNodeResponse(w http.ResponseWriter) error {
	w.Header().Add("content-type", "application/octet-stream")
	w.Header().Add("x-spacemesh-atx-nonce", fmt.Sprintf("%d", p.nonce))
	w.WriteHeader(200)
	if p.buf != nil {
		_, err := w.Write(p.buf)
		return err
	}
	return nil
}

func (s *Server) GetProposalLayerNode(ctx context.Context, request GetProposalLayerNodeRequestObject) (
	GetProposalLayerNodeResponseObject, error,
) {
	hexBuf, err := hex.DecodeString(request.Node)
	if err != nil {
		return &proposalResp{}, err
	}
	id := types.BytesToNodeID(hexBuf)

	proposal, nonce, err := s.proposals.BuildFor(ctx, types.LayerID(request.Layer), id)
	if err != nil {
		return &proposalResp{}, err
	}
	if proposal == nil {
		return &proposalResp{}, nil
	}
	// we have to explicitly check this case otherwise the next line may panic
	if proposal.Ballot.RefBallot != types.EmptyBallotID {
		return &proposalResp{buf: codec.MustEncode(proposal), nonce: nonce}, nil
	}
	if proposal.Ballot.EpochData.EligibilityCount == 0 {
		return &proposalResp{}, nil
	}
	return &proposalResp{buf: codec.MustEncode(proposal), nonce: nonce}, nil
}

type eligibilitySlotsResp struct {
	Slots uint32
	nonce types.VRFPostIndex
}

func (p *eligibilitySlotsResp) VisitGetEligibilitySlotsNodeEpochResponse(w http.ResponseWriter) error {
	w.Header().Add("content-type", "application/json")
	w.Header().Add("x-spacemesh-atx-nonce", fmt.Sprintf("%d", p.nonce))
	w.WriteHeader(200)

	if err := json.NewEncoder(w).Encode(p); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return err
	}
	return nil
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

	return &eligibilitySlotsResp{Slots: slots, nonce: nonce}, nil
}
