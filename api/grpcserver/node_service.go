package grpcserver

import (
	"context"

	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"go.uber.org/zap/zapcore"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NodeService is a grpc server that provides the NodeService, which exposes node-related
// data such as node status, software version, errors, etc. It can also be used to start
// the sync process, or to shut down the node.
type NodeService struct {
	Mesh        api.TxAPI
	GenTime     api.GenesisTimeAPI
	PeerCounter api.PeerCounter
	Syncer      api.Syncer
}

// RegisterService registers this service with a grpc server instance
func (s NodeService) RegisterService(server *Server) {
	pb.RegisterNodeServiceServer(server.GrpcServer, s)
}

// NewNodeService creates a new grpc service using config data.
func NewNodeService(
	net api.NetworkAPI, tx api.TxAPI, genTime api.GenesisTimeAPI, syncer api.Syncer) *NodeService {
	return &NodeService{
		Mesh:        tx,
		GenTime:     genTime,
		PeerCounter: peers.NewPeers(net, log.NewDefault("grpcserver.NodeService")),
		Syncer:      syncer,
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

// Version returns the version of the node software as a semver string
func (s NodeService) Version(context.Context, *empty.Empty) (*pb.VersionResponse, error) {
	log.Info("GRPC NodeService.Version")
	return &pb.VersionResponse{
		VersionString: &pb.SimpleString{Value: cmd.Version},
	}, nil
}

// Build returns the build of the node software
func (s NodeService) Build(context.Context, *empty.Empty) (*pb.BuildResponse, error) {
	log.Info("GRPC NodeService.Build")
	return &pb.BuildResponse{
		BuildString: &pb.SimpleString{Value: cmd.Commit},
	}, nil
}

// Status returns a status object providing information about the connected peers, sync status,
// current and verified layer
func (s NodeService) Status(ctx context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	log.Info("GRPC NodeService.Status")

	curLayer, latestLayer, verifiedLayer := s.getLayers()
	return &pb.StatusResponse{
		Status: &pb.NodeStatus{
			ConnectedPeers: s.PeerCounter.PeerCount(),              // number of connected peers
			IsSynced:       s.Syncer.IsSynced(ctx),                 // whether the node is synced
			SyncedLayer:    &pb.LayerNumber{Number: latestLayer},   // latest layer we saw from the network
			TopLayer:       &pb.LayerNumber{Number: curLayer},      // current layer, based on time
			VerifiedLayer:  &pb.LayerNumber{Number: verifiedLayer}, // latest verified layer
		},
	}, nil
}

func (s NodeService) getLayers() (curLayer, latestLayer, verifiedLayer uint32) {
	// We cannot get meaningful data from the mesh during the genesis epochs since there are no blocks in these
	// epochs, so just return the current layer instead
	curLayerObj := s.GenTime.GetCurrentLayer()
	curLayer = uint32(curLayerObj)
	if curLayerObj.GetEpoch().IsGenesis() {
		latestLayer = uint32(s.Mesh.LatestLayer())
		verifiedLayer = latestLayer
	} else {
		latestLayer = uint32(s.Mesh.LatestLayer())
		verifiedLayer = uint32(s.Mesh.LatestLayerInState())
	}
	return
}

// SyncStart requests that the node start syncing the mesh (if it isn't already syncing)
func (s NodeService) SyncStart(ctx context.Context, _ *pb.SyncStartRequest) (*pb.SyncStartResponse, error) {
	log.Info("GRPC NodeService.SyncStart")
	s.Syncer.Start(ctx)
	return &pb.SyncStartResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// Shutdown requests a graceful shutdown
func (s NodeService) Shutdown(context.Context, *pb.ShutdownRequest) (*pb.ShutdownResponse, error) {
	log.Info("GRPC NodeService.Shutdown")
	cmd.Cancel()
	return &pb.ShutdownResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// STREAMS

// StatusStream exposes a stream of node status updates
func (s NodeService) StatusStream(_ *pb.StatusStreamRequest, stream pb.NodeService_StatusStreamServer) error {
	log.Info("GRPC NodeService.StatusStream")
	statusStream := events.GetStatusChannel()

	for {
		select {
		case _, ok := <-statusStream:
			// statusStream works a bit differently than the other streams. It doesn't actually
			// send us data. Instead, it just notifies us that there's new data to be read.
			if !ok {
				log.Info("StatusStream closed, shutting down")
				return nil
			}
			curLayer, latestLayer, verifiedLayer := s.getLayers()
			if err := stream.Send(&pb.StatusStreamResponse{
				Status: &pb.NodeStatus{
					ConnectedPeers: s.PeerCounter.PeerCount(),              // number of connected peers
					IsSynced:       s.Syncer.IsSynced(stream.Context()),    // whether the node is synced
					SyncedLayer:    &pb.LayerNumber{Number: latestLayer},   // latest layer we saw from the network
					TopLayer:       &pb.LayerNumber{Number: curLayer},      // current layer, based on time
					VerifiedLayer:  &pb.LayerNumber{Number: verifiedLayer}, // latest verified layer
				},
			}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			log.Info("StatusStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}

// ErrorStream exposes a stream of node errors
func (s NodeService) ErrorStream(_ *pb.ErrorStreamRequest, stream pb.NodeService_ErrorStreamServer) error {
	log.Info("GRPC NodeService.ErrorStream")
	errorStream := events.GetErrorChannel()

	for {
		select {
		case nodeError, ok := <-errorStream:
			if !ok {
				log.Info("ErrorStream closed, shutting down")
				return nil
			}
			if err := stream.Send(&pb.ErrorStreamResponse{Error: &pb.NodeError{
				Level:      convertErrorLevel(nodeError.Level),
				Msg:        nodeError.Msg,
				StackTrace: nodeError.Trace,
			}}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			log.Info("ErrorStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}

// Convert internal error level into level understood by the API
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
