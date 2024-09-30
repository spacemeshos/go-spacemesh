package v2alpha1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
)

const (
	Account = "account_v2alpha1"
)

type accountConState interface {
	GetProjection(types.Address) (uint64, uint64)
}

func NewAccountService(db sql.Executor, conState accountConState) *AccountService {
	return &AccountService{
		db:       db,
		conState: conState,
	}
}

type AccountService struct {
	db       sql.Executor
	conState accountConState
}

func (s *AccountService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterAccountServiceServer(server, s)
}

func (s *AccountService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterAccountServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *AccountService) String() string {
	return "AccountService"
}

func (s *AccountService) List(
	_ context.Context,
	request *spacemeshv2alpha1.AccountRequest,
) (*spacemeshv2alpha1.AccountList, error) {
	switch {
	case request.Limit > 100:
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 100")
	case request.Limit == 0:
		return nil, status.Error(codes.InvalidArgument, "limit must be set to <= 100")
	}

	ops, err := toAccountOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	rst := make([]*spacemeshv2alpha1.Account, 0, request.Limit)
	if err := accounts.IterateAccountsOps(s.db, ops, func(account *types.Account) bool {
		counterProjected, balanceProjected := s.conState.GetProjection(account.Address)
		rst = append(rst, &spacemeshv2alpha1.Account{
			Address:  account.Address.String(),
			Template: account.TemplateAddress.String(),
			Current: &spacemeshv2alpha1.AccountState{
				Counter: account.NextNonce,
				Balance: account.Balance,
				Layer:   account.Layer.Uint32(),
			},
			Projected: &spacemeshv2alpha1.AccountState{
				Counter: counterProjected,
				Balance: balanceProjected,
			},
		})
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &spacemeshv2alpha1.AccountList{Accounts: rst}, nil
}

func toAccountOperations(filter *spacemeshv2alpha1.AccountRequest) (builder.Operations, error) {
	ops := builder.Operations{}
	if filter == nil {
		return ops, nil
	}

	if len(filter.Addresses) > 0 {
		var addrs [][]byte
		for _, address := range filter.Addresses {
			addr, err := types.StringToAddress(address)
			if err != nil {
				return builder.Operations{}, err
			}
			addrs = append(addrs, addr.Bytes())
		}

		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Address,
			Token: builder.In,
			Value: addrs,
		})
	}

	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.GroupBy,
		Value: "address",
	})

	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.OrderBy,
		Value: "layer_updated desc",
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
