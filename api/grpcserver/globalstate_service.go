package grpcserver

import (
	"bytes"
	"context"
	"fmt"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

// GlobalStateService exposes global state data, output from the STF.
type GlobalStateService struct {
	mesh     api.MeshAPI
	conState api.ConservativeState
}

// RegisterService registers this service with a grpc server instance.
func (s GlobalStateService) RegisterService(server *Server) {
	pb.RegisterGlobalStateServiceServer(server.GrpcServer, s)
}

// NewGlobalStateService creates a new grpc service using config data.
func NewGlobalStateService(msh api.MeshAPI, conState api.ConservativeState) *GlobalStateService {
	return &GlobalStateService{
		mesh:     msh,
		conState: conState,
	}
}

// GlobalStateHash returns the latest layer and its computed global state hash.
func (s GlobalStateService) GlobalStateHash(context.Context, *pb.GlobalStateHashRequest) (*pb.GlobalStateHashResponse, error) {
	log.Info("GRPC GlobalStateService.GlobalStateHash")
	return &pb.GlobalStateHashResponse{Response: &pb.GlobalStateHash{
		RootHash: s.conState.GetStateRoot().Bytes(),
		Layer:    &pb.LayerNumber{Number: s.mesh.LatestLayerInState().Uint32()},
	}}, nil
}

func (s GlobalStateService) getAccount(addr types.Address) (acct *pb.Account, err error) {
	balanceActual := s.conState.GetBalance(addr)
	counterActual := s.conState.GetNonce(addr)
	counterProjected, balanceProjected := s.conState.GetProjection(addr)
	return &pb.Account{
		AccountId: &pb.AccountId{Address: addr.Bytes()},
		StateCurrent: &pb.AccountState{
			Counter: counterActual,
			Balance: &pb.Amount{Value: balanceActual},
		},
		StateProjected: &pb.AccountState{
			Counter: counterProjected,
			Balance: &pb.Amount{Value: balanceProjected},
		},
	}, nil
}

// Account returns current and projected counter and balance for one account.
func (s GlobalStateService) Account(_ context.Context, in *pb.AccountRequest) (*pb.AccountResponse, error) {
	log.Info("GRPC GlobalStateService.Account")

	if in.AccountId == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`AccountId` must be provided")
	}

	// Load data
	addr := types.BytesToAddress(in.AccountId.Address)
	acct, err := s.getAccount(addr)
	if err != nil {
		log.With().Error("unable to fetch projected account state", log.Err(err))
		return nil, status.Errorf(codes.Internal, "error fetching projected account data")
	}

	log.With().Debug("GRPC GlobalStateService.Account",
		addr,
		log.Uint64("balance", acct.StateCurrent.Balance.Value),
		log.Uint64("counter", acct.StateCurrent.Counter),
		log.Uint64("balance projected", acct.StateProjected.Balance.Value),
		log.Uint64("counter projected", acct.StateProjected.Counter))

	return &pb.AccountResponse{AccountWrapper: acct}, nil
}

// AccountDataQuery returns historical account data such as rewards and receipts.
func (s GlobalStateService) AccountDataQuery(_ context.Context, in *pb.AccountDataQueryRequest) (*pb.AccountDataQueryResponse, error) {
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
	// filterTxReceipt := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_TRANSACTION_RECEIPT) != 0
	_ = in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_TRANSACTION_RECEIPT) != 0
	filterReward := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD) != 0
	filterAccount := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT) != 0

	addr := types.BytesToAddress(in.Filter.AccountId.Address)
	res := &pb.AccountDataQueryResponse{}

	// TODO: Implement this. The node does not implement tx receipts yet.
	// See https://github.com/spacemeshos/go-spacemesh/issues/2072
	// if filterTxReceipt {}

	if filterReward {
		dbRewards, err := s.mesh.GetRewards(addr)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "error getting rewards data")
		}
		for _, r := range dbRewards {
			res.AccountItem = append(res.AccountItem, &pb.AccountData{Datum: &pb.AccountData_Reward{
				Reward: &pb.Reward{
					Layer:       &pb.LayerNumber{Number: r.Layer.Uint32()},
					Total:       &pb.Amount{Value: r.TotalReward},
					LayerReward: &pb.Amount{Value: r.LayerRewardEstimate},
					// Leave this out for now as this is changing
					// See https://github.com/spacemeshos/go-spacemesh/issues/2275
					// LayerComputed: 0,
					Coinbase: &pb.AccountId{Address: addr.Bytes()},
					Smesher:  &pb.SmesherId{Id: r.SmesherID.ToBytes()},
				},
			}})
		}
	}

	if filterAccount {
		acct, err := s.getAccount(addr)
		if err != nil {
			log.With().Error("unable to fetch projected account state", log.Err(err))
			return nil, status.Errorf(codes.Internal, "error fetching projected account data")
		}
		res.AccountItem = append(res.AccountItem, &pb.AccountData{Datum: &pb.AccountData_AccountWrapper{
			AccountWrapper: acct,
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

// SmesherDataQuery returns historical info on smesher rewards.
func (s GlobalStateService) SmesherDataQuery(_ context.Context, in *pb.SmesherDataQueryRequest) (*pb.SmesherDataQueryResponse, error) {
	log.Info("GRPC GlobalStateService.SmesherDataQuery")

	if in.SmesherId == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Id` must be provided")
	}
	if in.SmesherId.Id == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Id.Id` must be provided")
	}

	smesherIDBytes := in.SmesherId.Id
	smesherID, err := types.BytesToNodeID(smesherIDBytes)
	if err != nil {
		log.With().Error("unable to convert bytes to nodeID", log.Err(err))
		return nil, status.Errorf(codes.Internal, "error deserializing NodeID")
	}

	dbRewards, err := s.mesh.GetRewardsBySmesherID(*smesherID)
	if err != nil {
		log.With().Error("unable to fetch projected reward state for smesher",
			log.FieldNamed("smesher_id", smesherID),
			log.Err(err))
		return nil, status.Errorf(codes.Internal, "error getting rewards data")
	}

	res := &pb.SmesherDataQueryResponse{}
	for _, r := range dbRewards {
		res.Rewards = append(res.Rewards, &pb.Reward{
			Layer:       &pb.LayerNumber{Number: r.Layer.Uint32()},
			Total:       &pb.Amount{Value: r.TotalReward},
			LayerReward: &pb.Amount{Value: r.LayerRewardEstimate},
			// Leave this out for now as this is changing
			// LayerComputed: 0,
			Coinbase: &pb.AccountId{Address: r.Coinbase.Bytes()},
			Smesher:  &pb.SmesherId{Id: r.SmesherID.ToBytes()},
		})
	}

	// MAX RESULTS, OFFSET
	// There is some code duplication here as this is implemented in other Query endpoints,
	// but without generics, there's no clean way to do this for different types.

	// Adjust for max results, offset
	res.TotalResults = uint32(len(res.Rewards))
	offset := in.Offset

	if offset > uint32(len(res.Rewards)) {
		return &pb.SmesherDataQueryResponse{}, nil
	}

	// If the max results is too high, trim it. If MaxResults is zero, that means unlimited
	// (since we have no way to distinguish between zero and its not being provided).
	maxResults := in.MaxResults
	if maxResults == 0 || offset+maxResults > uint32(len(res.Rewards)) {
		maxResults = uint32(len(res.Rewards)) - offset
	}
	res.Rewards = res.Rewards[offset : offset+maxResults]

	return res, nil
}

// STREAMS

// AccountDataStream exposes a stream of account-related data.
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
		accountCh       <-chan interface{}
		rewardsCh       <-chan interface{}
		receiptsCh      <-chan interface{}
		accountBufFull  <-chan struct{}
		rewardsBufFull  <-chan struct{}
		receiptsBufFull <-chan struct{}
	)
	if filterAccount {
		if accountSubscription := events.SubscribeAccount(); accountSubscription != nil {
			accountCh, accountBufFull = consumeEvents(stream.Context(), accountSubscription)
		}
	}
	if filterReward {
		if rewardsSubscription := events.SubscribeRewards(); rewardsSubscription != nil {
			rewardsCh, rewardsBufFull = consumeEvents(stream.Context(), rewardsSubscription)
		}
	}
	if filterReceipt {
		if receiptsSubscription := events.SubscribeReceipts(); receiptsSubscription != nil {
			receiptsCh, receiptsBufFull = consumeEvents(stream.Context(), receiptsSubscription)
		}
	}

	for {
		select {
		case <-accountBufFull:
			log.Info("account buffer is full, shutting down")
			return status.Error(codes.Canceled, errAccountBufferFull)
		case <-rewardsBufFull:
			log.Info("rewards buffer is full, shutting down")
			return status.Error(codes.Canceled, errRewardsBufferFull)
		case <-receiptsBufFull:
			log.Info("receipts buffer is full, shutting down")
			return status.Error(codes.Canceled, errReceiptsBufferFull)
		case updatedAccountEvent := <-accountCh:
			updatedAccount := updatedAccountEvent.(events.Account).Address
			// Apply address filter
			if updatedAccount == addr {
				// The Reporter service just sends us the account address. We are responsible
				// for looking up the other required data here. Get the account balance and
				// nonce.
				acct, err := s.getAccount(addr)
				if err != nil {
					log.With().Error("unable to fetch projected account state", log.Err(err))
					return status.Errorf(codes.Internal, "error fetching projected account data")
				}
				resp := &pb.AccountDataStreamResponse{Datum: &pb.AccountData{Datum: &pb.AccountData_AccountWrapper{
					AccountWrapper: acct,
				}}}
				if err := stream.Send(resp); err != nil {
					return fmt.Errorf("send to stream: %w", err)
				}
			}

		case rewardEvent := <-rewardsCh:
			reward := rewardEvent.(events.Reward)
			// Apply address filter
			if reward.Coinbase == addr {
				resp := &pb.AccountDataStreamResponse{Datum: &pb.AccountData{Datum: &pb.AccountData_Reward{
					Reward: &pb.Reward{
						Layer:       &pb.LayerNumber{Number: reward.Layer.Uint32()},
						Total:       &pb.Amount{Value: reward.Total},
						LayerReward: &pb.Amount{Value: reward.LayerReward},
						// Leave this out for now as this is changing
						// See https://github.com/spacemeshos/go-spacemesh/issues/2275
						// LayerComputed: 0,
						Coinbase: &pb.AccountId{Address: addr.Bytes()},
						Smesher:  &pb.SmesherId{Id: reward.Smesher.ToBytes()},
					},
				}}}
				if err := stream.Send(resp); err != nil {
					return fmt.Errorf("send to stream: %w", err)
				}
			}

		case receiptEvent := <-receiptsCh:
			receipt := receiptEvent.(events.TxReceipt)
			// Apply address filter
			if receipt.Address == addr {
				resp := &pb.AccountDataStreamResponse{Datum: &pb.AccountData{Datum: &pb.AccountData_Receipt{
					Receipt: &pb.TransactionReceipt{
						Id: &pb.TransactionId{Id: receipt.ID.Bytes()},
						// Result:      receipt.Result,
						GasUsed: receipt.GasUsed,
						Fee:     &pb.Amount{Value: receipt.Fee},
						Layer:   &pb.LayerNumber{Number: receipt.Layer.Uint32()},
						Index:   receipt.Index,
						// SvmData: nil,
					},
				}}}
				if err := stream.Send(resp); err != nil {
					return fmt.Errorf("send to stream: %w", err)
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

// SmesherRewardStream exposes a stream of smesher rewards.
func (s GlobalStateService) SmesherRewardStream(in *pb.SmesherRewardStreamRequest, stream pb.GlobalStateService_SmesherRewardStreamServer) error {
	log.Info("GRPC GlobalStateService.SmesherRewardStream")

	if in.Id == nil {
		return status.Errorf(codes.InvalidArgument, "`Id` must be provided")
	}
	if in.Id.Id == nil {
		return status.Errorf(codes.InvalidArgument, "`Id.Id` must be provided")
	}
	smesherIDBytes := in.Id.Id

	var (
		rewardsCh      <-chan interface{}
		rewardsBufFull <-chan struct{}
	)

	// subscribe to the rewards channel
	if rewardsSubscription := events.SubscribeRewards(); rewardsSubscription != nil {
		rewardsCh, rewardsBufFull = consumeEvents(stream.Context(), rewardsSubscription)
	}

	for {
		select {
		case <-rewardsBufFull:
			log.Info("rewards buffer is full, shutting down")
			return status.Error(codes.Canceled, errRewardsBufferFull)
		case rewardEvent, ok := <-rewardsCh:
			if !ok {
				// shut down the reward channel
				log.Info("Reward channel closed, shutting down")
				return nil
			}

			reward := rewardEvent.(events.Reward)

			// filter on the smesherID
			if comp := bytes.Compare(reward.Smesher.ToBytes(), smesherIDBytes); comp == 0 {
				resp := &pb.SmesherRewardStreamResponse{
					Reward: &pb.Reward{
						Layer:       &pb.LayerNumber{Number: reward.Layer.Uint32()},
						Total:       &pb.Amount{Value: reward.Total},
						LayerReward: &pb.Amount{Value: reward.LayerReward},
						// Leave this out for now as this is changing
						// LayerComputed: 0,
						Coinbase: &pb.AccountId{Address: reward.Coinbase.Bytes()},
						Smesher:  &pb.SmesherId{Id: reward.Smesher.ToBytes()},
					},
				}
				if err := stream.Send(resp); err != nil {
					return fmt.Errorf("send to stream: %w", err)
				}
			}
		case <-stream.Context().Done():
			log.Info("SmesherRewardStream closing stream, client disconnected")
			return nil
		}
	}
}

// AppEventStream exposes a stream of emitted app events.
func (s GlobalStateService) AppEventStream(*pb.AppEventStreamRequest, pb.GlobalStateService_AppEventStreamServer) error {
	log.Info("GRPC GlobalStateService.AppEventStream")

	// TODO: implement me! We don't currently have any app events
	// See https://github.com/spacemeshos/go-spacemesh/issues/2074

	return status.Errorf(codes.Unimplemented, "this endpoint has not yet been implemented")
}

// GlobalStateStream exposes a stream of global data data items: rewards, receipts, account info, global state hash.
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
		accountCh       <-chan interface{}
		rewardsCh       <-chan interface{}
		receiptsCh      <-chan interface{}
		layersCh        <-chan interface{}
		accountBufFull  <-chan struct{}
		rewardsBufFull  <-chan struct{}
		receiptsBufFull <-chan struct{}
		layersBufFull   <-chan struct{}
	)
	if filterAccount {
		if accountSubscription := events.SubscribeAccount(); accountSubscription != nil {
			accountCh, accountBufFull = consumeEvents(stream.Context(), accountSubscription)
		}
	}
	if filterReward {
		if rewardsSubscription := events.SubscribeRewards(); rewardsSubscription != nil {
			rewardsCh, rewardsBufFull = consumeEvents(stream.Context(), rewardsSubscription)
		}
	}
	if filterReceipt {
		if receiptsSubscription := events.SubscribeReceipts(); receiptsSubscription != nil {
			receiptsCh, receiptsBufFull = consumeEvents(stream.Context(), receiptsSubscription)
		}
	}
	if filterState {
		// Whenever new state is applied to the mesh, a new layer is reported.
		// There is no separate reporting specifically for new state.
		if layersSubscription := events.SubscribeLayers(); layersSubscription != nil {
			layersCh, layersBufFull = consumeEvents(stream.Context(), layersSubscription)
		}
	}

	for {
		select {
		case <-accountBufFull:
			log.Info("account buffer is full, shutting down")
			return status.Error(codes.Canceled, errAccountBufferFull)
		case <-rewardsBufFull:
			log.Info("rewards buffer is full, shutting down")
			return status.Error(codes.Canceled, errRewardsBufferFull)
		case <-receiptsBufFull:
			log.Info("receipts buffer is full, shutting down")
			return status.Error(codes.Canceled, errReceiptsBufferFull)
		case <-layersBufFull:
			log.Info("layers buffer is full, shutting down")
			return status.Error(codes.Canceled, errLayerBufferFull)
		case updatedAccountEvent := <-accountCh:
			updatedAccount := updatedAccountEvent.(events.Account).Address
			// The Reporter service just sends us the account address. We are responsible
			// for looking up the other required data here. Get the account balance and
			// nonce.
			acct, err := s.getAccount(updatedAccount)
			if err != nil {
				log.With().Error("unable to fetch projected account state", log.Err(err))
				return status.Errorf(codes.Internal, "error fetching projected account data")
			}
			resp := &pb.GlobalStateStreamResponse{Datum: &pb.GlobalStateData{Datum: &pb.GlobalStateData_AccountWrapper{
				AccountWrapper: acct,
			}}}
			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		case rewardEvent := <-rewardsCh:
			reward := rewardEvent.(events.Reward)

			resp := &pb.GlobalStateStreamResponse{Datum: &pb.GlobalStateData{Datum: &pb.GlobalStateData_Reward{
				Reward: &pb.Reward{
					Layer:       &pb.LayerNumber{Number: reward.Layer.Uint32()},
					Total:       &pb.Amount{Value: reward.Total},
					LayerReward: &pb.Amount{Value: reward.LayerReward},
					// Leave this out for now as this is changing
					// See https://github.com/spacemeshos/go-spacemesh/issues/2275
					// LayerComputed: 0,
					Coinbase: &pb.AccountId{Address: reward.Coinbase.Bytes()},
					Smesher:  &pb.SmesherId{Id: reward.Smesher.ToBytes()},
				},
			}}}
			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		case receiptEvent := <-receiptsCh:
			receipt := receiptEvent.(events.TxReceipt)

			resp := &pb.GlobalStateStreamResponse{Datum: &pb.GlobalStateData{Datum: &pb.GlobalStateData_Receipt{
				Receipt: &pb.TransactionReceipt{
					Id: &pb.TransactionId{Id: receipt.ID.Bytes()},
					// Result:      receipt.Result,
					GasUsed: receipt.GasUsed,
					Fee:     &pb.Amount{Value: receipt.Fee},
					Layer:   &pb.LayerNumber{Number: receipt.Layer.Uint32()},
					Index:   receipt.Index,
					// SvmData: nil,
				},
			}}}
			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		case layerEvent := <-layersCh:
			layer := layerEvent.(events.LayerUpdate)

			root, err := s.conState.GetLayerStateRoot(layer.LayerID)
			if err != nil {
				log.Error("error retrieving layer data: %s", err)
				return status.Errorf(codes.Internal, "error retrieving layer data")
			}
			resp := &pb.GlobalStateStreamResponse{Datum: &pb.GlobalStateData{Datum: &pb.GlobalStateData_GlobalState{
				GlobalState: &pb.GlobalStateHash{
					RootHash: root.Bytes(),
					Layer:    &pb.LayerNumber{Number: layer.LayerID.Uint32()},
				},
			}}}
			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("send to stream: %w", err)
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
