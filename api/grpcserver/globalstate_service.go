package grpcserver

import (
	"context"
	"fmt"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
)

// GlobalStateService exposes global state data, output from the STF.
type GlobalStateService struct {
	mesh     meshAPI
	conState conservativeState
}

// RegisterService registers this service with a grpc server instance.
func (s *GlobalStateService) RegisterService(server *grpc.Server) {
	pb.RegisterGlobalStateServiceServer(server, s)
}

func (s *GlobalStateService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterGlobalStateServiceHandlerServer(context.Background(), mux, s)
}

// String returns the name of the service.
func (s *GlobalStateService) String() string {
	return "GlobalStateService"
}

// NewGlobalStateService creates a new grpc service using config data.
func NewGlobalStateService(msh meshAPI, conState conservativeState) *GlobalStateService {
	return &GlobalStateService{
		mesh:     msh,
		conState: conState,
	}
}

// GlobalStateHash returns the latest layer and its computed global state hash.
func (s *GlobalStateService) GlobalStateHash(
	context.Context,
	*pb.GlobalStateHashRequest,
) (*pb.GlobalStateHashResponse, error) {
	root, err := s.conState.GetStateRoot()
	if err != nil {
		return nil, err
	}
	return &pb.GlobalStateHashResponse{Response: &pb.GlobalStateHash{
		RootHash: root.Bytes(),
		Layer:    &pb.LayerNumber{Number: s.mesh.LatestLayerInState().Uint32()},
	}}, nil
}

func (s *GlobalStateService) getAccount(addr types.Address) (acct *pb.Account, err error) {
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
func (s *GlobalStateService) Account(ctx context.Context, in *pb.AccountRequest) (*pb.AccountResponse, error) {
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
		ctxzap.Error(ctx, "unable to fetch projected account state", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "error fetching projected account data")
	}

	ctxzap.Debug(ctx, "GRPC GlobalStateService.Account",
		zap.Stringer("address", addr),
		zap.Uint64("balance", acct.StateCurrent.Balance.Value),
		zap.Uint64("counter", acct.StateCurrent.Counter),
		zap.Uint64("balance projected", acct.StateProjected.Balance.Value),
		zap.Uint64("counter projected", acct.StateProjected.Counter),
	)

	return &pb.AccountResponse{AccountWrapper: acct}, nil
}

// AccountDataQuery returns historical account data such as rewards and receipts.
func (s *GlobalStateService) AccountDataQuery(
	ctx context.Context,
	in *pb.AccountDataQueryRequest,
) (*pb.AccountDataQueryResponse, error) {
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
		dbRewards, err := s.mesh.GetRewardsByCoinbase(addr)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "error getting rewards data")
		}
		for _, r := range dbRewards {
			res.AccountItem = append(res.AccountItem, &pb.AccountData{Datum: &pb.AccountData_Reward{
				Reward: &pb.Reward{
					Layer:       &pb.LayerNumber{Number: r.Layer.Uint32()},
					Total:       &pb.Amount{Value: r.TotalReward},
					LayerReward: &pb.Amount{Value: r.LayerReward},
					Coinbase:    &pb.AccountId{Address: addr.String()},
					Smesher:     &pb.SmesherId{Id: r.SmesherID[:]},
				},
			}})
		}
	}

	if filterAccount {
		acct, err := s.getAccount(addr)
		if err != nil {
			ctxzap.Error(ctx, "unable to fetch projected account state", zap.Error(err))
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
func (s *GlobalStateService) SmesherDataQuery(
	_ context.Context,
	in *pb.SmesherDataQueryRequest,
) (*pb.SmesherDataQueryResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "DEPRECATED")
}

// STREAMS

// AccountDataStream exposes a stream of account-related data.
func (s *GlobalStateService) AccountDataStream(
	in *pb.AccountDataStreamRequest,
	stream pb.GlobalStateService_AccountDataStreamServer,
) error {
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
		rewardsCh      <-chan types.Reward
		receiptsCh     <-chan any
		accountBufFull <-chan struct{}
		rewardsBufFull <-chan struct{}
	)
	if filterAccount {
		accountSubscription, err := events.SubscribeAccount()
		if err != nil {
			return status.Errorf(codes.Internal, "error subscribing to account events: %v", err)
		}
		if accountSubscription != nil {
			accountCh, accountBufFull = consumeEvents[events.Account](stream.Context(), accountSubscription)
		}
	}
	if filterReward {
		rewardsSubscription, err := events.SubscribeRewards()
		if err != nil {
			return status.Errorf(codes.Internal, "error subscribing to rewards events: %v", err)
		}
		if rewardsSubscription != nil {
			rewardsCh, rewardsBufFull = consumeEvents[types.Reward](stream.Context(), rewardsSubscription)
		}
	}
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}

	for {
		select {
		case <-accountBufFull:
			ctxzap.Info(stream.Context(), "account buffer is full, shutting down")
			return status.Error(codes.Canceled, errAccountBufferFull)
		case <-rewardsBufFull:
			ctxzap.Info(stream.Context(), "rewards buffer is full, shutting down")
			return status.Error(codes.Canceled, errRewardsBufferFull)
		case updatedAccountEvent := <-accountCh:
			// Apply address filter
			if updatedAccountEvent.Address == addr {
				// The Reporter service just sends us the account address. We are responsible
				// for looking up the other required data here. Get the account balance and
				// nonce.
				acct, err := s.getAccount(addr)
				if err != nil {
					ctxzap.Error(stream.Context(), "unable to fetch projected account state", zap.Error(err))
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
						Total:       &pb.Amount{Value: reward.TotalReward},
						LayerReward: &pb.Amount{Value: reward.LayerReward},
						Coinbase:    &pb.AccountId{Address: addr.String()},
						Smesher:     &pb.SmesherId{Id: reward.SmesherID[:]},
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
			ctxzap.Info(stream.Context(), "AccountDataStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
		// See https://github.com/spacemeshos/go-spacemesh/issues/2075
	}
}

// SmesherRewardStream exposes a stream of smesher rewards.
func (s *GlobalStateService) SmesherRewardStream(
	in *pb.SmesherRewardStreamRequest,
	stream pb.GlobalStateService_SmesherRewardStreamServer,
) error {
	return status.Errorf(codes.Unimplemented, "DEPRECATED")
}

// AppEventStream exposes a stream of emitted app events.
func (s *GlobalStateService) AppEventStream(
	*pb.AppEventStreamRequest,
	pb.GlobalStateService_AppEventStreamServer,
) error {
	// TODO: implement me! We don't currently have any app events
	// See https://github.com/spacemeshos/go-spacemesh/issues/2074

	return status.Errorf(codes.Unimplemented, "this endpoint has not yet been implemented")
}

// GlobalStateStream exposes a stream of global data data items: rewards, receipts, account info, global state hash.
func (s *GlobalStateService) GlobalStateStream(
	in *pb.GlobalStateStreamRequest,
	stream pb.GlobalStateService_GlobalStateStreamServer,
) error {
	if in.GlobalStateDataFlags == uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_UNSPECIFIED) {
		return status.Errorf(codes.InvalidArgument, "`GlobalStateDataFlags` must set at least one bitfield")
	}

	filterAccount := in.GlobalStateDataFlags&uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_ACCOUNT) != 0
	filterReward := in.GlobalStateDataFlags&uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_REWARD) != 0
	filterState := in.GlobalStateDataFlags&uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_GLOBAL_STATE_HASH) != 0

	// Subscribe to the various streams
	var (
		accountCh      <-chan events.Account
		rewardsCh      <-chan types.Reward
		layersCh       <-chan events.LayerUpdate
		accountBufFull <-chan struct{}
		rewardsBufFull <-chan struct{}
		layersBufFull  <-chan struct{}
	)
	if filterAccount {
		accountSubscription, err := events.SubscribeAccount()
		if err != nil {
			return status.Errorf(codes.Internal, "error subscribing to account events: %v", err)
		}
		if accountSubscription != nil {
			accountCh, accountBufFull = consumeEvents[events.Account](stream.Context(), accountSubscription)
		}
	}
	if filterReward {
		rewardsSubscription, err := events.SubscribeRewards()
		if err != nil {
			return status.Errorf(codes.Internal, "error subscribing to rewards events: %v", err)
		}
		if rewardsSubscription != nil {
			rewardsCh, rewardsBufFull = consumeEvents[types.Reward](stream.Context(), rewardsSubscription)
		}
	}

	if filterState {
		// Whenever new state is applied to the mesh, a new layer is reported.
		// There is no separate reporting specifically for new state.
		layersSubscription, err := events.SubscribeLayers()
		if err != nil {
			return status.Errorf(codes.Internal, "error subscribing to layer updates: %v", err)
		}
		if layersSubscription != nil {
			layersCh, layersBufFull = consumeEvents[events.LayerUpdate](stream.Context(), layersSubscription)
		}
	}

	for {
		select {
		case <-accountBufFull:
			ctxzap.Info(stream.Context(), "account buffer is full, shutting down")
			return status.Error(codes.Canceled, errAccountBufferFull)
		case <-rewardsBufFull:
			ctxzap.Info(stream.Context(), "rewards buffer is full, shutting down")
			return status.Error(codes.Canceled, errRewardsBufferFull)
		case <-layersBufFull:
			ctxzap.Info(stream.Context(), "layers buffer is full, shutting down")
			return status.Error(codes.Canceled, errLayerBufferFull)
		case updatedAccount := <-accountCh:
			// The Reporter service just sends us the account address. We are responsible
			// for looking up the other required data here. Get the account balance and
			// nonce.
			acct, err := s.getAccount(updatedAccount.Address)
			if err != nil {
				ctxzap.Error(stream.Context(), "unable to fetch projected account state", zap.Error(err))
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
					Total:       &pb.Amount{Value: reward.TotalReward},
					LayerReward: &pb.Amount{Value: reward.LayerReward},
					Coinbase:    &pb.AccountId{Address: reward.Coinbase.String()},
					Smesher:     &pb.SmesherId{Id: reward.SmesherID[:]},
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
				ctxzap.Warn(stream.Context(), "error retrieving layer data", zap.Error(err))
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
			ctxzap.Info(stream.Context(), "AccountDataStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
		// See https://github.com/spacemeshos/go-spacemesh/issues/2075
	}
}
