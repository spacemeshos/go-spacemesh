package grpcserver

import (
	"context"
	"fmt"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

// GlobalStateService exposes global state data, output from the STF.
type GlobalStateService struct {
	logger   log.Logger
	mesh     meshAPI
	conState conservativeState
}

// RegisterService registers this service with a grpc server instance.
func (s GlobalStateService) RegisterService(server *Server) {
	pb.RegisterGlobalStateServiceServer(server.GrpcServer, s)
}

// NewGlobalStateService creates a new grpc service using config data.
func NewGlobalStateService(msh meshAPI, conState conservativeState, lg log.Logger) *GlobalStateService {
	return &GlobalStateService{
		logger:   lg,
		mesh:     msh,
		conState: conState,
	}
}

// GlobalStateHash returns the latest layer and its computed global state hash.
func (s GlobalStateService) GlobalStateHash(context.Context, *pb.GlobalStateHashRequest) (*pb.GlobalStateHashResponse, error) {
	s.logger.Info("GRPC GlobalStateService.GlobalStateHash")

	root, err := s.conState.GetStateRoot()
	if err != nil {
		return nil, err
	}
	return &pb.GlobalStateHashResponse{Response: &pb.GlobalStateHash{
		RootHash: root.Bytes(),
		Layer:    &pb.LayerNumber{Number: s.mesh.LatestLayerInState().Uint32()},
	}}, nil
}

func (s GlobalStateService) getAccount(addr types.Address) (acct *pb.Account, err error) {
	balanceActual, err := s.conState.GetBalance(addr)
	if err != nil {
		return nil, err
	}
	counterActual, err := s.conState.GetNonce(addr)
	if err != nil {
		return nil, err
	}
	counterProjected, balanceProjected := s.conState.GetProjection(addr)
	return &pb.Account{
		AccountId: &pb.AccountId{Address: addr.String()},
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
	s.logger.Info("GRPC GlobalStateService.Account")

	if in.AccountId == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`AccountId` must be provided")
	}

	// Load data
	addr, err := types.StringToAddress(in.AccountId.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse in.AccountId.Address `%s`: %w", in.AccountId.Address, err)
	}
	acct, err := s.getAccount(addr)
	if err != nil {
		s.logger.With().Error("unable to fetch projected account state", log.Err(err))
		return nil, status.Errorf(codes.Internal, "error fetching projected account data")
	}

	s.logger.With().Debug("GRPC GlobalStateService.Account",
		addr,
		log.Uint64("balance", acct.StateCurrent.Balance.Value),
		log.Uint64("counter", acct.StateCurrent.Counter),
		log.Uint64("balance projected", acct.StateProjected.Balance.Value),
		log.Uint64("counter projected", acct.StateProjected.Counter),
	)

	return &pb.AccountResponse{AccountWrapper: acct}, nil
}

// AccountDataQuery returns historical account data such as rewards and receipts.
func (s GlobalStateService) AccountDataQuery(_ context.Context, in *pb.AccountDataQueryRequest) (*pb.AccountDataQueryResponse, error) {
	s.logger.Info("GRPC GlobalStateService.AccountDataQuery")

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

	addr, err := types.StringToAddress(in.Filter.AccountId.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse in.Filter.AccountId.Address `%s`: %w", in.Filter.AccountId.Address, err)
	}
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
					LayerReward: &pb.Amount{Value: r.LayerReward},
					// Leave this out for now as this is changing
					// See https://github.com/spacemeshos/go-spacemesh/issues/2275
					// LayerComputed: 0,
					Coinbase: &pb.AccountId{Address: addr.String()},
				},
			}})
		}
	}

	if filterAccount {
		acct, err := s.getAccount(addr)
		if err != nil {
			s.logger.With().Error("unable to fetch projected account state", log.Err(err))
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
	s.logger.Info("DEPRECATED GRPC GlobalStateService.SmesherDataQuery")
	return nil, status.Errorf(codes.Unimplemented, "DEPRECATED")
}

// STREAMS

// AccountDataStream exposes a stream of account-related data.
func (s GlobalStateService) AccountDataStream(in *pb.AccountDataStreamRequest, stream pb.GlobalStateService_AccountDataStreamServer) error {
	s.logger.Info("GRPC GlobalStateService.AccountDataStream")

	if in.Filter == nil {
		return status.Errorf(codes.InvalidArgument, "`Filter` must be provided")
	}
	if in.Filter.AccountId == nil {
		return status.Errorf(codes.InvalidArgument, "`Filter.AccountId` must be provided")
	}
	if in.Filter.AccountDataFlags == uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_UNSPECIFIED) {
		return status.Errorf(codes.InvalidArgument, "`Filter.AccountDataFlags` must set at least one bitfield")
	}
	addr, err := types.StringToAddress(in.Filter.AccountId.Address)
	if err != nil {
		return fmt.Errorf("failed to parse in.Filter.AccountId.Address `%s`: %w", in.Filter.AccountId.Address, err)
	}

	filterAccount := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT) != 0
	filterReward := in.Filter.AccountDataFlags&uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD) != 0

	// Subscribe to the various streams
	var (
		accountCh      <-chan events.Account
		rewardsCh      <-chan events.Reward
		receiptsCh     <-chan any
		accountBufFull <-chan struct{}
		rewardsBufFull <-chan struct{}
	)
	if filterAccount {
		if accountSubscription := events.SubscribeAccount(); accountSubscription != nil {
			accountCh, accountBufFull = consumeEvents[events.Account](stream.Context(), accountSubscription)
		}
	}
	if filterReward {
		if rewardsSubscription := events.SubscribeRewards(); rewardsSubscription != nil {
			rewardsCh, rewardsBufFull = consumeEvents[events.Reward](stream.Context(), rewardsSubscription)
		}
	}
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}

	for {
		select {
		case <-accountBufFull:
			s.logger.Info("account buffer is full, shutting down")
			return status.Error(codes.Canceled, errAccountBufferFull)
		case <-rewardsBufFull:
			s.logger.Info("rewards buffer is full, shutting down")
			return status.Error(codes.Canceled, errRewardsBufferFull)
		case updatedAccountEvent := <-accountCh:
			// Apply address filter
			if updatedAccountEvent.Address == addr {
				// The Reporter service just sends us the account address. We are responsible
				// for looking up the other required data here. Get the account balance and
				// nonce.
				acct, err := s.getAccount(addr)
				if err != nil {
					s.logger.With().Error("unable to fetch projected account state", log.Err(err))
					return status.Errorf(codes.Internal, "error fetching projected account data")
				}
				resp := &pb.AccountDataStreamResponse{Datum: &pb.AccountData{Datum: &pb.AccountData_AccountWrapper{
					AccountWrapper: acct,
				}}}
				if err := stream.Send(resp); err != nil {
					return fmt.Errorf("send to stream: %w", err)
				}
			}

		case reward := <-rewardsCh:
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
						Coinbase: &pb.AccountId{Address: addr.String()},
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
			s.logger.Info("AccountDataStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
		// See https://github.com/spacemeshos/go-spacemesh/issues/2075
	}
}

// SmesherRewardStream exposes a stream of smesher rewards.
func (s GlobalStateService) SmesherRewardStream(in *pb.SmesherRewardStreamRequest, stream pb.GlobalStateService_SmesherRewardStreamServer) error {
	s.logger.Info("DEPRECATED GRPC GlobalStateService.SmesherRewardStream")
	return status.Errorf(codes.Unimplemented, "DEPRECATED")
}

// AppEventStream exposes a stream of emitted app events.
func (s GlobalStateService) AppEventStream(*pb.AppEventStreamRequest, pb.GlobalStateService_AppEventStreamServer) error {
	s.logger.Info("GRPC GlobalStateService.AppEventStream")

	// TODO: implement me! We don't currently have any app events
	// See https://github.com/spacemeshos/go-spacemesh/issues/2074

	return status.Errorf(codes.Unimplemented, "this endpoint has not yet been implemented")
}

// GlobalStateStream exposes a stream of global data data items: rewards, receipts, account info, global state hash.
func (s GlobalStateService) GlobalStateStream(in *pb.GlobalStateStreamRequest, stream pb.GlobalStateService_GlobalStateStreamServer) error {
	s.logger.Info("GRPC GlobalStateService.GlobalStateStream")

	if in.GlobalStateDataFlags == uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_UNSPECIFIED) {
		return status.Errorf(codes.InvalidArgument, "`GlobalStateDataFlags` must set at least one bitfield")
	}

	filterAccount := in.GlobalStateDataFlags&uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_ACCOUNT) != 0
	filterReward := in.GlobalStateDataFlags&uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_REWARD) != 0
	filterState := in.GlobalStateDataFlags&uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_GLOBAL_STATE_HASH) != 0

	// Subscribe to the various streams
	var (
		accountCh      <-chan events.Account
		rewardsCh      <-chan events.Reward
		layersCh       <-chan events.LayerUpdate
		accountBufFull <-chan struct{}
		rewardsBufFull <-chan struct{}
		layersBufFull  <-chan struct{}
	)
	if filterAccount {
		if accountSubscription := events.SubscribeAccount(); accountSubscription != nil {
			accountCh, accountBufFull = consumeEvents[events.Account](stream.Context(), accountSubscription)
		}
	}
	if filterReward {
		if rewardsSubscription := events.SubscribeRewards(); rewardsSubscription != nil {
			rewardsCh, rewardsBufFull = consumeEvents[events.Reward](stream.Context(), rewardsSubscription)
		}
	}

	if filterState {
		// Whenever new state is applied to the mesh, a new layer is reported.
		// There is no separate reporting specifically for new state.
		if layersSubscription := events.SubscribeLayers(); layersSubscription != nil {
			layersCh, layersBufFull = consumeEvents[events.LayerUpdate](stream.Context(), layersSubscription)
		}
	}

	for {
		select {
		case <-accountBufFull:
			s.logger.Info("account buffer is full, shutting down")
			return status.Error(codes.Canceled, errAccountBufferFull)
		case <-rewardsBufFull:
			s.logger.Info("rewards buffer is full, shutting down")
			return status.Error(codes.Canceled, errRewardsBufferFull)
		case <-layersBufFull:
			s.logger.Info("layers buffer is full, shutting down")
			return status.Error(codes.Canceled, errLayerBufferFull)
		case updatedAccount := <-accountCh:
			// The Reporter service just sends us the account address. We are responsible
			// for looking up the other required data here. Get the account balance and
			// nonce.
			acct, err := s.getAccount(updatedAccount.Address)
			if err != nil {
				s.logger.With().Error("unable to fetch projected account state", log.Err(err))
				return status.Errorf(codes.Internal, "error fetching projected account data")
			}
			resp := &pb.GlobalStateStreamResponse{Datum: &pb.GlobalStateData{Datum: &pb.GlobalStateData_AccountWrapper{
				AccountWrapper: acct,
			}}}
			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		case reward := <-rewardsCh:
			resp := &pb.GlobalStateStreamResponse{Datum: &pb.GlobalStateData{Datum: &pb.GlobalStateData_Reward{
				Reward: &pb.Reward{
					Layer:       &pb.LayerNumber{Number: reward.Layer.Uint32()},
					Total:       &pb.Amount{Value: reward.Total},
					LayerReward: &pb.Amount{Value: reward.LayerReward},
					// Leave this out for now as this is changing
					// See https://github.com/spacemeshos/go-spacemesh/issues/2275
					// LayerComputed: 0,
					Coinbase: &pb.AccountId{Address: reward.Coinbase.String()},
				},
			}}}
			if err := stream.Send(resp); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		case layer := <-layersCh:
			if layer.Status != events.LayerStatusTypeApplied {
				continue
			}
			root, err := s.conState.GetLayerStateRoot(layer.LayerID)
			if err != nil {
				s.logger.With().Warning("error retrieving layer data", log.Err(err))
				root = types.Hash32{}
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
			s.logger.Info("AccountDataStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
		// See https://github.com/spacemeshos/go-spacemesh/issues/2075
	}
}
