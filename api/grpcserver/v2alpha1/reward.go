package v2alpha1

import (
	"context"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	Reward = "reward_v2alpha1"
)

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
	ctx context.Context,
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
		rst = append(rst, &spacemeshv2alpha1.Reward{Versioned: &spacemeshv2alpha1.Reward_V1{V1: toReward(reward)}})
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &spacemeshv2alpha1.RewardList{Rewards: rst}, nil
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
		Value: "layer asc",
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

func toReward(reward *types.Reward) *spacemeshv2alpha1.RewardV1 {
	return &spacemeshv2alpha1.RewardV1{
		Layer:       reward.Layer.Uint32(),
		Total:       reward.TotalReward,
		LayerReward: reward.LayerReward,
		Coinbase:    reward.Coinbase.String(),
		Smesher:     reward.SmesherID.Bytes(),
	}
}
