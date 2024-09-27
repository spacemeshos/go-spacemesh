package v2alpha1

import (
	"context"
	"errors"
	"io"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
)

const (
	Reward       = "reward_v2alpha1"
	RewardStream = "reward_stream_v2alpha1"
)

func NewRewardStreamService(db sql.Executor) *RewardStreamService {
	return &RewardStreamService{db: db}
}

type RewardStreamService struct {
	db sql.Executor
}

func (s *RewardStreamService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterRewardStreamServiceServer(server, s)
}

func (s *RewardStreamService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterRewardStreamServiceHandlerServer(context.Background(), mux, s)
}

func (s *RewardStreamService) Stream(
	request *spacemeshv2alpha1.RewardStreamRequest,
	stream spacemeshv2alpha1.RewardStreamService_StreamServer,
) error {
	ctx := stream.Context()
	var sub *events.BufferedSubscription[types.Reward]
	if request.Watch {
		matcher := rewardsMatcher{request, ctx}
		var err error
		sub, err = events.SubscribeMatched(matcher.match)
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		defer sub.Close()
		if err := stream.SendHeader(metadata.MD{}); err != nil {
			return status.Errorf(codes.Unavailable, "can't send header")
		}
	}

	ops, err := toRewardOperations(toRewardRequest(request))
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	dbChan, errChan := s.fetchFromDB(ctx, ops)

	var eventsOut <-chan types.Reward
	var eventsFull <-chan struct{}
	if sub != nil {
		eventsOut = sub.Out()
		eventsFull = sub.Full()
	}

	for {
		select {
		case rst := <-eventsOut:
			err := stream.Send(toReward(&rst))
			switch {
			case errors.Is(err, io.EOF):
				return nil
			case err != nil:
				return status.Error(codes.Internal, err.Error())
			}
		default:
			select {
			case rst := <-eventsOut:
				err := stream.Send(toReward(&rst))
				switch {
				case errors.Is(err, io.EOF):
					return nil
				case err != nil:
					return status.Error(codes.Internal, err.Error())
				}
			case <-eventsFull:
				return status.Error(codes.Canceled, "buffer overflow")
			case rst, ok := <-dbChan:
				if !ok {
					dbChan = nil
					if sub == nil {
						return nil
					}
					continue
				}
				err := stream.Send(toReward(rst))
				switch {
				case errors.Is(err, io.EOF):
					return nil
				case err != nil:
					return status.Error(codes.Internal, err.Error())
				}
			case err := <-errChan:
				return err
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (s *RewardStreamService) fetchFromDB(
	ctx context.Context,
	ops builder.Operations,
) (<-chan *types.Reward, <-chan error) {
	dbChan := make(chan *types.Reward)
	errChan := make(chan error, 1) // buffered to avoid blocking, routine should exit immediately after sending an error

	go func() {
		defer close(dbChan)
		if err := rewards.IterateRewardsOps(s.db, ops, func(rwd *types.Reward) bool {
			select {
			case dbChan <- rwd:
				return true
			case <-ctx.Done():
				// exit if the context is canceled
				return false
			}
		}); err != nil {
			errChan <- status.Error(codes.Internal, err.Error())
		}
	}()

	return dbChan, errChan
}

func (s *RewardStreamService) String() string {
	return "RewardStreamService"
}

func NewRewardService(db sql.Executor) *RewardService {
	return &RewardService{db: db}
}

type RewardService struct {
	db sql.Executor
}

func (s *RewardService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterRewardServiceServer(server, s)
}

func (s *RewardService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterRewardServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *RewardService) String() string {
	return "RewardService"
}

func (s *RewardService) List(
	_ context.Context,
	request *spacemeshv2alpha1.RewardRequest,
) (*spacemeshv2alpha1.RewardList, error) {
	ops, err := toRewardOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	switch {
	case request.Limit > 100:
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 100")
	case request.Limit == 0:
		return nil, status.Error(codes.InvalidArgument, "limit must be set to <= 100")
	}

	rst := make([]*spacemeshv2alpha1.Reward, 0, request.Limit)
	if err := rewards.IterateRewardsOps(s.db, ops, func(reward *types.Reward) bool {
		rst = append(rst, toReward(reward))
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &spacemeshv2alpha1.RewardList{Rewards: rst}, nil
}

func toRewardRequest(filter *spacemeshv2alpha1.RewardStreamRequest) *spacemeshv2alpha1.RewardRequest {
	req := &spacemeshv2alpha1.RewardRequest{
		StartLayer: filter.StartLayer,
		EndLayer:   filter.EndLayer,
	}

	if filter.GetCoinbase() != "" {
		req.FilterBy = &spacemeshv2alpha1.RewardRequest_Coinbase{Coinbase: filter.GetCoinbase()}
	}

	if len(filter.GetSmesher()) > 0 {
		req.FilterBy = &spacemeshv2alpha1.RewardRequest_Smesher{Smesher: filter.GetSmesher()}
	}

	return req
}

func toRewardOperations(filter *spacemeshv2alpha1.RewardRequest) (builder.Operations, error) {
	ops := builder.Operations{}
	if filter == nil {
		return ops, nil
	}

	if filter.GetCoinbase() != "" {
		addr, err := types.StringToAddress(filter.GetCoinbase())
		if err != nil {
			return builder.Operations{}, err
		}

		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Coinbase,
			Token: builder.Eq,
			Value: addr.Bytes(),
		})
	}

	if len(filter.GetSmesher()) > 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Smesher,
			Token: builder.Eq,
			Value: filter.GetSmesher(),
		})
	}

	if filter.StartLayer != 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Layer,
			Token: builder.Gte,
			Value: int64(filter.StartLayer),
		})
	}
	if filter.EndLayer != 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Layer,
			Token: builder.Lte,
			Value: int64(filter.EndLayer),
		})
	}

	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.OrderBy,
		Value: "layer " + filter.SortOrder.String(),
	})

	if filter.Limit != 0 {
		ops.Modifiers = append(ops.Modifiers, builder.Modifier{
			Key:   builder.Limit,
			Value: int64(filter.Limit),
		})
	}
	if filter.Offset != 0 {
		ops.Modifiers = append(ops.Modifiers, builder.Modifier{
			Key:   builder.Offset,
			Value: int64(filter.Offset),
		})
	}

	return ops, nil
}

func toReward(reward *types.Reward) *spacemeshv2alpha1.Reward {
	return &spacemeshv2alpha1.Reward{
		Layer:       reward.Layer.Uint32(),
		Total:       reward.TotalReward,
		LayerReward: reward.LayerReward,
		Coinbase:    reward.Coinbase.String(),
		Smesher:     reward.SmesherID.Bytes(),
	}
}

type rewardsMatcher struct {
	*spacemeshv2alpha1.RewardStreamRequest
	ctx context.Context
}

func (m *rewardsMatcher) match(t *types.Reward) bool {
	if len(m.GetSmesher()) > 0 {
		var nodeId types.NodeID
		copy(nodeId[:], m.GetSmesher())

		if t.SmesherID != nodeId {
			return false
		}
	}

	if m.GetCoinbase() != "" {
		addr, err := types.StringToAddress(m.GetCoinbase())
		if err != nil {
			ctxzap.Error(m.ctx, "unable to convert reward coinbase", zap.Error(err))
			return false
		}
		if t.Coinbase != addr {
			return false
		}
	}

	if m.StartLayer != 0 {
		if t.Layer.Uint32() < m.StartLayer {
			return false
		}
	}

	if m.EndLayer != 0 {
		if t.Layer.Uint32() > m.EndLayer {
			return false
		}
	}

	return true
}
