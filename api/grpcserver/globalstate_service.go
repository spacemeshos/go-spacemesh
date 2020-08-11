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
		PeerCounter: peers.NewPeers(net, log.NewDefault("grpcserver.GlobalStateService")),
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

	if in.AccountId == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`AccountId` must be provided")
	}

	// Load data
	addr := types.BytesToAddress(in.AccountId.Address)
	balance := s.Mesh.GetBalance(addr)
	counter := s.Mesh.GetNonce(addr)

	log.With().Debug("GRPC GlobalStateService.Account",
		addr, log.Uint64("balance", balance), log.Uint64("counter", counter))

	return &pb.AccountResponse{AccountWrapper: &pb.Account{
		AccountId: &pb.AccountId{Address: addr.Bytes()},
		Counter:   counter,
		Balance:   &pb.Amount{Value: balance},
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
		// See https://github.com/spacemeshos/go-spacemesh/issues/2072
	}

	if filterReward {
		dbRewards, err := s.Mesh.GetRewards(addr)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "error getting rewards data")
		}
		for _, r := range dbRewards {
			res.AccountItem = append(res.AccountItem, &pb.AccountData{Datum: &pb.AccountData_Reward{
				Reward: &pb.Reward{
					Layer:       r.Layer.Uint64(),
					Total:       &pb.Amount{Value: r.TotalReward},
					LayerReward: &pb.Amount{Value: r.LayerRewardEstimate},
					// Leave this out for now as this is changing
					//LayerComputed: 0,
					Coinbase: &pb.AccountId{Address: addr.Bytes()},
					// TODO: There is currently no way to get this for a reward.
					// See https://github.com/spacemeshos/go-spacemesh/issues/2068
					//Smesher:  nil,
				},
			}})
		}
	}

	if filterAccount {
		balance := s.Mesh.GetBalance(addr)
		counter := s.Mesh.GetNonce(addr)
		res.AccountItem = append(res.AccountItem, &pb.AccountData{Datum: &pb.AccountData_AccountWrapper{
			AccountWrapper: &pb.Account{
				AccountId: &pb.AccountId{Address: addr.Bytes()},
				Counter:   counter,
				Balance:   &pb.Amount{Value: balance},
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
	// See https://github.com/spacemeshos/go-spacemesh/issues/2073
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
	// See https://github.com/spacemeshos/go-spacemesh/issues/2068

	return nil, status.Errorf(codes.Unimplemented, "this endpoint has not yet been implemented")
}

// STREAMS

// AccountDataStream exposes a stream of account-related data
func (s GlobalStateService) AccountDataStream(in *pb.AccountDataStreamRequest, stream pb.GlobalStateService_AccountDataStreamServer) error {
	log.Info("GRPC GlobalStateService.AccountDataStream")

	if in.Filter == nil {
		return status.Errorf(codes.InvalidArgument, "`Filter` must be provided")
	}
	if in.Filter.AccountId == nil {
		return status.Errorf(codes.InvalidArgument, "`Filter.AccountId` must be provided")
	}
	if in.Filter.AccountDataFlags == uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_UNSPECIFIED) {
		return status.Errorf(codes.InvalidArgument, "`Filter.AccountDataFlags` must set at least one bitfield")
	}
	addr := types.BytesToAddress(in.Filter.AccountId.Address)

	filterAccount := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT) != 0
	filterReward := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD) != 0
	filterReceipt := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_TRANSACTION_RECEIPT) != 0

	// Subscribe to the various streams
	var (
		channelAccount chan types.Address
		channelReward  chan events.Reward
		channelReceipt chan events.TxReceipt
	)
	if filterAccount {
		channelAccount = events.GetAccountChannel()
	}
	if filterReward {
		channelReward = events.GetRewardChannel()
	}
	if filterReceipt {
		channelReceipt = events.GetReceiptChannel()
	}

	for {
		select {
		case updatedAccount, ok := <-channelAccount:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("account channel closed, shutting down")
				return nil
			}
			// Apply address filter
			if updatedAccount == addr {
				// The Reporter service just sends us the account address. We are responsible
				// for looking up the other required data here. Get the account balance and
				// nonce.
				balance := s.Mesh.GetBalance(addr)
				counter := s.Mesh.GetNonce(addr)
				if err := stream.Send(&pb.AccountDataStreamResponse{Datum: &pb.AccountData{Datum: &pb.AccountData_AccountWrapper{
					AccountWrapper: &pb.Account{
						AccountId: &pb.AccountId{Address: addr.Bytes()},
						Counter:   counter,
						Balance:   &pb.Amount{Value: balance},
					},
				}}}); err != nil {
					return err
				}
			}
		case reward, ok := <-channelReward:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("reward channel closed, shutting down")
				return nil
			}
			// Apply address filter
			if reward.Coinbase == addr {
				if err := stream.Send(&pb.AccountDataStreamResponse{Datum: &pb.AccountData{Datum: &pb.AccountData_Reward{
					Reward: &pb.Reward{
						Layer:       reward.Layer.Uint64(),
						Total:       &pb.Amount{Value: reward.Total},
						LayerReward: &pb.Amount{Value: reward.LayerReward},
						// Leave this out for now as this is changing
						//LayerComputed: 0,
						Coinbase: &pb.AccountId{Address: addr.Bytes()},
						// TODO: There is currently no way to get this for a reward.
						// See https://github.com/spacemeshos/go-spacemesh/issues/2068
						//Smesher:  nil,
					},
				}}}); err != nil {
					return err
				}
			}
		case receipt, ok := <-channelReceipt:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("receipt channel closed, shutting down")
				return nil
			}
			// Apply address filter
			if receipt.Address == addr {
				if err := stream.Send(&pb.AccountDataStreamResponse{Datum: &pb.AccountData{Datum: &pb.AccountData_Receipt{
					Receipt: &pb.TransactionReceipt{
						Id: &pb.TransactionId{Id: receipt.ID.Bytes()},
						//Result:      receipt.Result,
						GasUsed:     receipt.GasUsed,
						Fee:         &pb.Amount{Value: receipt.Fee},
						LayerNumber: receipt.Layer.Uint64(),
						Index:       receipt.Index,
						AppAddress:  &pb.AccountId{Address: receipt.Address.Bytes()},
					},
				}}}); err != nil {
					return err
				}
			}
		case <-stream.Context().Done():
			log.Info("AccountDataStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
		// See https://github.com/spacemeshos/go-spacemesh/issues/2075
	}
}

// SmesherRewardStream exposes a stream of smesher rewards
func (s GlobalStateService) SmesherRewardStream(request *pb.SmesherRewardStreamRequest, stream pb.GlobalStateService_SmesherRewardStreamServer) error {
	log.Info("GRPC GlobalStateService.SmesherRewardStream")

	// TODO: implement me! We don't currently have a way to read rewards per-smesher.
	// See https://github.com/spacemeshos/go-spacemesh/issues/2068

	return status.Errorf(codes.Unimplemented, "this endpoint has not yet been implemented")
}

// AppEventStream exposes a stream of emitted app events
func (s GlobalStateService) AppEventStream(request *pb.AppEventStreamRequest, stream pb.GlobalStateService_AppEventStreamServer) error {
	log.Info("GRPC GlobalStateService.AppEventStream")

	// TODO: implement me! We don't currently have any app events
	// See https://github.com/spacemeshos/go-spacemesh/issues/2074

	return status.Errorf(codes.Unimplemented, "this endpoint has not yet been implemented")
}

// GlobalStateStream exposes a stream of global data data items: rewards, receipts, account info, global state hash
func (s GlobalStateService) GlobalStateStream(in *pb.GlobalStateStreamRequest, stream pb.GlobalStateService_GlobalStateStreamServer) error {
	log.Info("GRPC GlobalStateService.GlobalStateStream")

	if in.GlobalStateDataFlags == uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_UNSPECIFIED) {
		return status.Errorf(codes.InvalidArgument, "`GlobalStateDataFlags` must set at least one bitfield")
	}

	filterAccount := in.GlobalStateDataFlags&uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_ACCOUNT) != 0
	filterReward := in.GlobalStateDataFlags&uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_REWARD) != 0
	filterReceipt := in.GlobalStateDataFlags&uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_TRANSACTION_RECEIPT) != 0
	filterState := in.GlobalStateDataFlags&uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_GLOBAL_STATE_HASH) != 0

	// Subscribe to the various streams
	var (
		channelAccount chan types.Address
		channelReward  chan events.Reward
		channelReceipt chan events.TxReceipt
		channelLayer   chan events.NewLayer
	)
	if filterAccount {
		channelAccount = events.GetAccountChannel()
	}
	if filterReward {
		channelReward = events.GetRewardChannel()
	}
	if filterReceipt {
		channelReceipt = events.GetReceiptChannel()
	}
	if filterState {
		// Whenever new state is applied to the mesh, a new layer is reported.
		// There is no separate reporting specifically for new state.
		channelLayer = events.GetLayerChannel()
	}

	for {
		select {
		case updatedAccount, ok := <-channelAccount:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("account channel closed, shutting down")
				return nil
			}
			// The Reporter service just sends us the account address. We are responsible
			// for looking up the other required data here. Get the account balance and
			// nonce.
			balance := s.Mesh.GetBalance(updatedAccount)
			counter := s.Mesh.GetNonce(updatedAccount)
			if err := stream.Send(&pb.GlobalStateStreamResponse{Datum: &pb.GlobalStateData{Datum: &pb.GlobalStateData_AccountWrapper{
				AccountWrapper: &pb.Account{
					AccountId: &pb.AccountId{Address: updatedAccount.Bytes()},
					Counter:   counter,
					Balance:   &pb.Amount{Value: balance},
				},
			}}}); err != nil {
				return err
			}
		case reward, ok := <-channelReward:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("reward channel closed, shutting down")
				return nil
			}
			if err := stream.Send(&pb.GlobalStateStreamResponse{Datum: &pb.GlobalStateData{Datum: &pb.GlobalStateData_Reward{
				Reward: &pb.Reward{
					Layer:       reward.Layer.Uint64(),
					Total:       &pb.Amount{Value: reward.Total},
					LayerReward: &pb.Amount{Value: reward.LayerReward},
					// Leave this out for now as this is changing
					//LayerComputed: 0,
					Coinbase: &pb.AccountId{Address: reward.Coinbase.Bytes()},
					// TODO: There is currently no way to get this for a reward.
					// See https://github.com/spacemeshos/go-spacemesh/issues/2068
					//Smesher:  nil,
				},
			}}}); err != nil {
				return err
			}
		case receipt, ok := <-channelReceipt:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("receipt channel closed, shutting down")
				return nil
			}
			if err := stream.Send(&pb.GlobalStateStreamResponse{Datum: &pb.GlobalStateData{Datum: &pb.GlobalStateData_Receipt{
				Receipt: &pb.TransactionReceipt{
					Id: &pb.TransactionId{Id: receipt.ID.Bytes()},
					//Result:      receipt.Result,
					GasUsed:     receipt.GasUsed,
					Fee:         &pb.Amount{Value: receipt.Fee},
					LayerNumber: receipt.Layer.Uint64(),
					Index:       receipt.Index,
					AppAddress:  &pb.AccountId{Address: receipt.Address.Bytes()},
				},
			}}}); err != nil {
				return err
			}
		case layer, ok := <-channelLayer:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("layer channel closed, shutting down")
				return nil
			}
			root, err := s.Mesh.GetLayerStateRoot(layer.Layer.Index())
			if err != nil {
				log.Error("error retrieving layer data: %s", err)
				return status.Errorf(codes.Internal, "error retrieving layer data")
			}
			if err := stream.Send(&pb.GlobalStateStreamResponse{Datum: &pb.GlobalStateData{Datum: &pb.GlobalStateData_GlobalState{
				GlobalState: &pb.GlobalStateHash{
					RootHash:    root.Bytes(),
					LayerNumber: layer.Layer.Index().Uint64(),
				},
			}}}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			log.Info("AccountDataStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
		// See https://github.com/spacemeshos/go-spacemesh/issues/2075
	}
}
