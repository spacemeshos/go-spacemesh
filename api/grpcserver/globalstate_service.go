package grpcserver

import (
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GlobalStateService exposes global state data, output from the STF
type GlobalStateService struct {
	Network     api.NetworkAPI // P2P Swarm
	Mesh        api.TxAPI      // Mesh
	GenTime     api.GenesisTimeAPI
	PeerCounter api.PeerCounter
	Syncer      api.Syncer
}

// RegisterService registers this service with a grpc server instance
func (s GlobalStateService) RegisterService(server *Server) {
	pb.RegisterGlobalStateServiceServer(server.GrpcServer, s)
}

// NewGlobalStateService creates a new grpc service using config data.
func NewGlobalStateService(
	net api.NetworkAPI, tx api.TxAPI, genTime api.GenesisTimeAPI,
	syncer api.Syncer) *GlobalStateService {
	return &GlobalStateService{
		Network:     net,
		Mesh:        tx,
		GenTime:     genTime,
		PeerCounter: peers.NewPeers(net, log.NewDefault("grpc_server.GlobalStateService")),
		Syncer:      syncer,
	}
}

// GlobalStateHash returns the latest layer and its computed global state hash
func (s GlobalStateService) GlobalStateHash(ctx context.Context, in *pb.GlobalStateHashRequest) (*pb.GlobalStateHashResponse, error) {
	log.Info("GRPC GlobalStateService.GlobalStateHash")
	return &pb.GlobalStateHashResponse{Response: &pb.GlobalStateHash{
		RootHash:    s.Mesh.GetStateRoot().Bytes(),
		LayerNumber: s.Mesh.LatestLayerInState().Uint64(),
	}}, nil
}

// Account returns data for one account
func (s GlobalStateService) Account(ctx context.Context, in *pb.AccountRequest) (*pb.AccountResponse, error) {
	log.Info("GRPC GlobalStateService.Account")

	// Load data
	addr := types.BytesToAddress(in.AccountId.Address)
	balance := s.Mesh.GetBalance(addr)
	counter := s.Mesh.GetNonce(addr)

	return &pb.AccountResponse{Account: &pb.Account{
		Address: &pb.AccountId{Address: addr.Bytes()},
		Counter: counter,
		Balance: &pb.Amount{Value: balance},
	}}, nil
}

// AccountDataQuery returns historical account data such as rewards and receipts
func (s GlobalStateService) AccountDataQuery(ctx context.Context, in *pb.AccountDataQueryRequest) (*pb.AccountDataQueryResponse, error) {
	log.Info("GRPC GlobalStateService.AccountDataQuery")

	if in.Filter == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Filter` must be provided")
	}
	if in.Filter.AccountId == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Filter.AccountId` must be provided")
	}
	if in.Filter.AccountDataFlags == uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_UNSPECIFIED) {
		return nil, status.Errorf(codes.InvalidArgument, "`Filter.AccountMeshDataFlags` must set at least one bitfield")
	}

	// Read the filter flags
	filterTxReceipt := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_TRANSACTION_RECEIPT) != 0
	filterReward := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD) != 0
	filterAccount := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT) != 0

	addr := types.BytesToAddress(in.Filter.AccountId.Address)
	res := &pb.AccountDataQueryResponse{}

	if filterTxReceipt {
		// TODO: Implement this. The node does not implement tx receipts yet.
	}

	if filterReward {
		dbRewards, err := s.Mesh.GetRewards(addr)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "error getting rewards data")
		}
		for _, r := range dbRewards {
			res.AccountItem = append(res.AccountItem, &pb.AccountData{Item: &pb.AccountData_Reward{
				Reward: &pb.Reward{
					Layer:       r.Layer.Uint64(),
					Total:       &pb.Amount{Value: r.TotalReward},
					LayerReward: &pb.Amount{Value: r.LayerRewardEstimate},
					// Leave this out for now as this is changing
					//LayerComputed: 0,
					Coinbase: &pb.AccountId{Address: addr.Bytes()},
					// TODO: There is currently no way to get this for a reward.
					// See https://github.com/spacemeshos/go-spacemesh/issues/2068.
					//Smesher:  nil,
				},
			}})
		}
	}

	if filterAccount {
		balance := s.Mesh.GetBalance(addr)
		counter := s.Mesh.GetNonce(addr)
		res.AccountItem = append(res.AccountItem, &pb.AccountData{Item: &pb.AccountData_Account{
			Account: &pb.Account{
				Address: &pb.AccountId{Address: addr.Bytes()},
				Counter: counter,
				Balance: &pb.Amount{Value: balance},
			},
		}})
	}

	// MAX RESULTS, OFFSET
	// There is some code duplication here as this is implemented in other Query endpoints,
	// but without generics, there's no clean way to do this for different types.

	// Adjust for max results, offset
	res.TotalResults = uint32(len(res.AccountItem))

	// Skip to offset, don't send more than max results
	// TODO: Optimize this. Obviously, we could do much smarter things than re-loading all
	// of the data from scratch, then figuring out which data to return here. We could cache
	// query results and/or figure out which data to load before loading it.
	offset := in.Offset

	// If the offset is too high there is nothing to return (this is not an error)
	if offset > uint32(len(res.AccountItem)) {
		return &pb.AccountDataQueryResponse{}, nil
	}

	// If the max results is too high, trim it. If MaxResults is zero, that means unlimited
	// (since we have no way to distinguish between zero and its not being provided).
	maxResults := in.MaxResults
	if maxResults == 0 || offset+maxResults > uint32(len(res.AccountItem)) {
		maxResults = uint32(len(res.AccountItem)) - offset
	}
	res.AccountItem = res.AccountItem[offset : offset+maxResults]
	return res, nil
}

// SmesherDataQuery returns historical info on smesher rewards
func (s GlobalStateService) SmesherDataQuery(ctx context.Context, in *pb.SmesherDataQueryRequest) (*pb.SmesherDataQueryResponse, error) {
	log.Info("GRPC GlobalStateService.SmesherDataQuery")

	// TODO: implement me! We don't currently have a way to read rewards per-smesher.
	// See https://github.com/spacemeshos/go-spacemesh/issues/2068.

	return nil, status.Errorf(codes.Unimplemented, "this endpoint has not yet been implemented")
}

// STREAMS

// AccountDataStream exposes a stream of account-related data
func (s GlobalStateService) AccountDataStream(request *pb.AccountDataStreamRequest, stream pb.GlobalStateService_AccountDataStreamServer) error {
	log.Info("GRPC GlobalStateService.AccountDataStream")

	addr := types.BytesToAddress(request.Filter.AccountId.Address)

	channelAccount := events.GetAccountChannel()
	channelReward := events.GetRewardChannel()

	for {
		select {
		case acct, ok := <-channelAccount:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("channelAccount closed, shutting down")
				return nil
			}
			// Apply address filter
			if acct.Address == addr {
				//pbActivation, err := convertActivation(activation)
				//if err != nil {
				//	log.Error("error serializing activation data: ", err)
				//	return status.Errorf(codes.Internal, "error serializing activation data")
				//}
				//if err := stream.Send(&pb.AccountMeshDataStreamResponse{
				//	Data: &pb.AccountMeshData{
				//		DataItem: &pb.AccountMeshData_Activation{
				//			Activation: pbActivation,
				//		},
				//	},
				//}); err != nil {
				//	return err
				//}
			}
		case reward, ok := <-channelReward:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("channelReward closed, shutting down")
				return nil
			}
			// Apply address filter
			if reward.Coinbase == addr {

			}
			//if tx.Origin() == addr || tx.Recipient == addr {
			//	if err := stream.Send(&pb.AccountMeshDataStreamResponse{
			//		Data: &pb.AccountMeshData{
			//			DataItem: &pb.AccountMeshData_Transaction{
			//				Transaction: convertTransaction(tx),
			//			},
			//		},
			//	}); err != nil {
			//		return err
			//	}
			//}
		case <-stream.Context().Done():
			log.Info("AccountDataStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}

// SmesherRewardStream exposes a stream of smesher rewards
func (s GlobalStateService) SmesherRewardStream(request *pb.SmesherRewardStreamRequest, stream pb.GlobalStateService_SmesherRewardStreamServer) error {
	log.Info("GRPC GlobalStateService.SmesherRewardStream")

	// TODO: implement me! We don't currently have a way to read rewards per-smesher.
	// See https://github.com/spacemeshos/go-spacemesh/issues/2068.

	return status.Errorf(codes.Unimplemented, "this endpoint has not yet been implemented")
}

// AppEventStream exposes a stream of emitted app events
func (s GlobalStateService) AppEventStream(request *pb.AppEventStreamRequest, stream pb.GlobalStateService_AppEventStreamServer) error {
	log.Info("GRPC GlobalStateService.AppEventStream")

	// TODO: implement me! We don't currently have any app events

	return status.Errorf(codes.Unimplemented, "this endpoint has not yet been implemented")
}

// GlobalStateStream exposes a stream of global data data items: rewards, receipts, account info, global state hash
func (s GlobalStateService) GlobalStateStream(request *pb.GlobalStateStreamRequest, stream pb.GlobalStateService_GlobalStateStreamServer) error {
	log.Info("GRPC GlobalStateService.GlobalStateStream")
	return nil
}
