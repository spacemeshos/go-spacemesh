package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/node/models"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

type Server struct {
	activationService activation.AtxService
	publisher         pubsub.Publisher
	logger            *zap.Logger
}

var _ StrictServerInterface = (*Server)(nil)

func NewServer(activationService activation.AtxService, publisher pubsub.PubSub, logger *zap.Logger) *Server {
	return &Server{
		activationService: activationService,
		publisher:         publisher,
		logger:            logger,
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
	atx, err := s.activationService.Atx(ctx, id)
	switch {
	case errors.Is(err, common.ErrNotFound):
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

	atxid, err := s.activationService.LastATX(ctx, id)
	switch {
	case errors.Is(err, common.ErrNotFound):
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
	id, err := s.activationService.PositioningATX(ctx, types.EpochID(request.PublishEpoch))
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

	s.publisher.Publish(ctx, string(request.Protocol), blob)
	return PostPublishProtocol200Response{}, nil
}
