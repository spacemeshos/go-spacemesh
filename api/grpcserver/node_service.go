package grpcserver

import (
	"context"
	"fmt"

	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NodeService is a grpc server that provides the NodeService, which exposes node-related
// data such as node status, software version, errors, etc. It can also be used to start
// the sync process, or to shut down the node.
type NodeService struct {
	appCtx      context.Context
	mesh        meshAPI
	genTime     genesisTimeAPI
	peerCounter peerCounter
	syncer      syncer
	appVersion  string
	appCommit   string
}

// RegisterService registers this service with a grpc server instance.
func (s NodeService) RegisterService(server *Server) {
	pb.RegisterNodeServiceServer(server.GrpcServer, s)
}

// NewNodeService creates a new grpc service using config data.
func NewNodeService(
	appCtx context.Context,
	peers peerCounter,
	msh meshAPI,
	genTime genesisTimeAPI,
	syncer syncer,
	appVersion string,
	appCommit string,
) *NodeService {
	return &NodeService{
		appCtx:      appCtx,
		mesh:        msh,
		genTime:     genTime,
		peerCounter: peers,
		syncer:      syncer,
		appVersion:  appVersion,
		appCommit:   appCommit,
	}
}

// Echo returns the response for an echo api request. It's used for E2E tests.
func (s NodeService) Echo(_ context.Context, in *pb.EchoRequest) (*pb.EchoResponse, error) {
	log.Info("GRPC NodeService.Echo")
	if in.Msg != nil {
		return &pb.EchoResponse{Msg: &pb.SimpleString{Value: in.Msg.Value}}, nil
	}
	return nil, status.Errorf(codes.InvalidArgument, "Must include `Msg`")
}

// Version returns the version of the node software as a semver string.
func (s NodeService) Version(context.Context, *empty.Empty) (*pb.VersionResponse, error) {
	log.Info("GRPC NodeService.Version")
	return &pb.VersionResponse{
		VersionString: &pb.SimpleString{Value: s.appVersion},
	}, nil
}

// Build returns the build of the node software.
func (s NodeService) Build(context.Context, *empty.Empty) (*pb.BuildResponse, error) {
	log.Info("GRPC NodeService.Build")
	return &pb.BuildResponse{
		BuildString: &pb.SimpleString{Value: s.appCommit},
	}, nil
}

// Status returns a status object providing information about the connected peers, sync status,
// current and verified layer.
func (s NodeService) Status(ctx context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	log.Info("GRPC NodeService.Status")

	curLayer, latestLayer, verifiedLayer := s.getLayers()
	return &pb.StatusResponse{
		Status: &pb.NodeStatus{
			ConnectedPeers: s.peerCounter.PeerCount(),              // number of connected peers
			IsSynced:       s.syncer.IsSynced(ctx),                 // whether the node is synced
			SyncedLayer:    &pb.LayerNumber{Number: latestLayer},   // latest layer we saw from the network
			TopLayer:       &pb.LayerNumber{Number: curLayer},      // current layer, based on time
			VerifiedLayer:  &pb.LayerNumber{Number: verifiedLayer}, // latest verified layer
		},
	}, nil
}

func (s NodeService) getLayers() (curLayer, latestLayer, verifiedLayer uint32) {
	// We cannot get meaningful data from the mesh during the genesis epochs since there are no blocks in these
	// epochs, so just return the current layer instead
	curLayerObj := s.genTime.CurrentLayer()
	curLayer = curLayerObj.Uint32()
	if curLayerObj <= types.GetEffectiveGenesis() {
		latestLayer = s.mesh.LatestLayer().Uint32()
		verifiedLayer = latestLayer
	} else {
		latestLayer = s.mesh.LatestLayer().Uint32()
		verifiedLayer = s.mesh.LatestLayerInState().Uint32()
	}
	return
}

// STREAMS

// StatusStream exposes a stream of node status updates.
func (s NodeService) StatusStream(_ *pb.StatusStreamRequest, stream pb.NodeService_StatusStreamServer) error {
	log.Info("GRPC NodeService.StatusStream")

	var (
		statusCh      <-chan events.Status
		statusBufFull <-chan struct{}
	)

	if statusSubscription := events.SubscribeStatus(); statusSubscription != nil {
		statusCh, statusBufFull = consumeEvents[events.Status](stream.Context(), statusSubscription)
	}

	for {
		select {
		case <-statusBufFull:
			log.Info("status buffer is full, shutting down")
			return status.Error(codes.Canceled, errStatusBufferFull)
		case _, ok := <-statusCh:
			// statusCh works a bit differently than the other streams. It doesn't actually
			// send us data. Instead, it just notifies us that there's new data to be read.
			if !ok {
				log.Info("StatusStream closed, shutting down")
				return nil
			}
			curLayer, latestLayer, verifiedLayer := s.getLayers()

			resp := &pb.StatusStreamResponse{
				Status: &pb.NodeStatus{
					ConnectedPeers: s.peerCounter.PeerCount(),              // number of connected peers
					IsSynced:       s.syncer.IsSynced(stream.Context()),    // whether the node is synced
					SyncedLayer:    &pb.LayerNumber{Number: latestLayer},   // latest layer we saw from the network
					TopLayer:       &pb.LayerNumber{Number: curLayer},      // current layer, based on time
					VerifiedLayer:  &pb.LayerNumber{Number: verifiedLayer}, // latest verified layer
				},
			}

			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		case <-stream.Context().Done():
			log.Info("StatusStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}

// ErrorStream exposes a stream of node errors.
func (s NodeService) ErrorStream(_ *pb.ErrorStreamRequest, stream pb.NodeService_ErrorStreamServer) error {
	log.Info("GRPC NodeService.ErrorStream")

	var (
		errorsCh      <-chan events.NodeError
		errorsBufFull <-chan struct{}
	)

	if errorsSubscription := events.SubscribeErrors(); errorsSubscription != nil {
		errorsCh, errorsBufFull = consumeEvents[events.NodeError](stream.Context(), errorsSubscription)
	}
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return status.Errorf(codes.Unavailable, "can't send header")
	}

	for {
		select {
		case <-errorsBufFull:
			log.Info("errors buffer is full, shutting down")
			return status.Error(codes.Canceled, errErrorsBufferFull)
		case nodeError, ok := <-errorsCh:
			if !ok {
				log.Info("ErrorStream closed, shutting down")
				return nil
			}

			resp := &pb.ErrorStreamResponse{Error: &pb.NodeError{
				Level:      convertErrorLevel(nodeError.Level),
				Msg:        nodeError.Msg,
				StackTrace: nodeError.Trace,
			}}

			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		case <-stream.Context().Done():
			log.Info("ErrorStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}

// Convert internal error level into level understood by the API.
func convertErrorLevel(level zapcore.Level) pb.LogLevel {
	switch level {
	case zapcore.DebugLevel:
		return pb.LogLevel_LOG_LEVEL_DEBUG
	case zapcore.InfoLevel:
		return pb.LogLevel_LOG_LEVEL_INFO
	case zapcore.WarnLevel:
		return pb.LogLevel_LOG_LEVEL_WARN
	case zapcore.ErrorLevel:
		return pb.LogLevel_LOG_LEVEL_ERROR
	case zapcore.DPanicLevel:
		return pb.LogLevel_LOG_LEVEL_DPANIC
	case zapcore.PanicLevel:
		return pb.LogLevel_LOG_LEVEL_PANIC
	case zapcore.FatalLevel:
		return pb.LogLevel_LOG_LEVEL_FATAL
	default:
		return pb.LogLevel_LOG_LEVEL_UNSPECIFIED
	}
}
