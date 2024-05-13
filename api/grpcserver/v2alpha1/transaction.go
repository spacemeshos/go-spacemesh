package v2alpha1

import (
	"context"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	Transaction       = "transaction_v2alpha1"
	TransactionStream = "transaction_stream_v2alpha1"
)

func NewTransactionStreamService(db sql.Executor) *TransactionStreamService {
	return &TransactionStreamService{db: db}
}

type TransactionStreamService struct {
	db sql.Executor
}

func (s *TransactionStreamService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterTransactionStreamServiceServer(server, s)
}

func (s *TransactionStreamService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterTransactionStreamServiceHandlerServer(context.Background(), mux, s)
}

func (s *TransactionStreamService) Stream(
	request *spacemeshv2alpha1.TransactionStreamRequest,
	stream spacemeshv2alpha1.TransactionStreamService_StreamServer,
) error {
	return nil
}

func (s *TransactionStreamService) String() string {
	return "TransactionStreamService"
}

func NewTransactionService(db sql.Executor) *TransactionService {
	return &TransactionService{db: db}
}

type TransactionService struct {
	db sql.Executor
}

func (s *TransactionService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterTransactionServiceServer(server, s)
}

func (s *TransactionService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterTransactionServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *TransactionService) String() string {
	return "TransactionService"
}

func (s *TransactionService) List(
	ctx context.Context,
	request *spacemeshv2alpha1.TransactionRequest,
) (*spacemeshv2alpha1.TransactionList, error) {
	ops, err := toTransactionOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	switch {
	case request.Limit > 100:
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 100")
	case request.Limit == 0:
		return nil, status.Error(codes.InvalidArgument, "limit must be set to <= 100")
	}

	rst := make([]*spacemeshv2alpha1.TransactionResponse, 0, request.Limit)
	if err := transactions.IterateTransactionsOps(s.db, ops, func(tx *types.MeshTransaction, result *types.TransactionResult) bool {
		rst = append(rst, toTx(tx, result, request.IncludeResult, request.IncludeState))
		return true
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &spacemeshv2alpha1.TransactionList{Transactions: rst}, nil
}

func (s *TransactionService) ParseTransaction(
	ctx context.Context,
	request *spacemeshv2alpha1.ParseTransactionRequest,
) (*spacemeshv2alpha1.ParseTransactionResponse, error) {
	return &spacemeshv2alpha1.ParseTransactionResponse{}, nil
}

func (s *TransactionService) SubmitTransaction(
	ctx context.Context,
	request *spacemeshv2alpha1.SubmitTransactionRequest,
) (*spacemeshv2alpha1.SubmitTransactionResponse, error) {
	return &spacemeshv2alpha1.SubmitTransactionResponse{}, nil
}

func (s *TransactionService) EstimateGas(
	ctx context.Context,
	request *spacemeshv2alpha1.EstimateGasRequest,
) (*spacemeshv2alpha1.EstimateGasResponse, error) {
	return &spacemeshv2alpha1.EstimateGasResponse{}, nil
}

func toTransactionOperations(filter *spacemeshv2alpha1.TransactionRequest) (builder.Operations, error) {
	ops := builder.Operations{}
	if filter == nil {
		return ops, nil
	}

	if filter.GetPrincipal() != "" {
		addr, err := types.StringToAddress(filter.GetPrincipal())
		if err != nil {
			return builder.Operations{}, err
		}
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Address,
			Token: builder.Eq,
			Value: addr.Bytes(),
		})
	}

	if filter.StartLayer != nil {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Layer,
			Token: builder.Gte,
			Value: int64(filter.GetStartLayer()),
		})
	}

	if filter.EndLayer != nil {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Address,
			Token: builder.Lte,
			Value: int64(filter.GetEndLayer()),
		})
	}

	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.OrderBy,
		Value: "layer asc, id",
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

func toTx(tx *types.MeshTransaction, result *types.TransactionResult, includeResult bool, includeState bool) *spacemeshv2alpha1.TransactionResponse {
	rst := &spacemeshv2alpha1.TransactionResponse{}

	t := &spacemeshv2alpha1.TransactionV1{
		Id:  tx.ID.Bytes(),
		Raw: tx.Raw,
	}

	if tx.TxHeader != nil {
		t.Principal = tx.Principal.String()
		t.Template = tx.TemplateAddress.String()
		t.Method = uint32(tx.Method)
		t.Nonce = &spacemeshv2alpha1.Nonce{Counter: tx.Nonce}
		t.MaxGas = tx.MaxGas
		t.GasPrice = tx.GasPrice
		t.MaxSpend = tx.MaxSpend
		t.Contents = &spacemeshv2alpha1.TransactionContents{}

		// TODO: decode tx raw
		switch tx.Method {
		case core.MethodSpawn:
			t.Contents.Contents = &spacemeshv2alpha1.TransactionContents_SingleSigSpawn{
				SingleSigSpawn: &spacemeshv2alpha1.ContentsSingleSigSpawn{
					Pubkey: "",
				},
			}
		case core.MethodSpend:
			t.Contents.Contents = &spacemeshv2alpha1.TransactionContents_Send{
				Send: &spacemeshv2alpha1.ContentsSend{
					Destination: "",
					Amount:      0,
				},
			}
		}
	}

	if includeResult {
		rst.TxResult = &spacemeshv2alpha1.TransactionResult{
			Status:           convertTxResult(result),
			Message:          result.Message,
			GasConsumed:      result.Gas,
			Fee:              result.Fee,
			Block:            result.Block[:],
			Layer:            result.Layer.Uint32(),
			TouchedAddresses: nil,
		}
		if len(result.Addresses) > 0 {
			rst.TxResult.TouchedAddresses = make([]string, len(result.Addresses))
			for i := range result.Addresses {
				rst.TxResult.TouchedAddresses[i] = result.Addresses[i].String()
			}
		}
	}

	if includeState {
		rst.TxState = convertTxState(tx)
	}

	rst.Tx = &spacemeshv2alpha1.Transaction{Versioned: &spacemeshv2alpha1.Transaction_V1{V1: t}}

	return rst
}

func convertTxResult(result *types.TransactionResult) spacemeshv2alpha1.TransactionResult_Status {
	switch result.Status {
	case types.TransactionSuccess:
		return spacemeshv2alpha1.TransactionResult_TRANSACTION_STATUS_SUCCESS
	case types.TransactionFailure:
		return spacemeshv2alpha1.TransactionResult_TRANSACTION_STATUS_FAILURE
	default:
		return spacemeshv2alpha1.TransactionResult_TRANSACTION_STATUS_UNSPECIFIED
	}
}

// TODO: REJECTED, INSUFFICIENT_FUNDS, CONFLICTING, MESH
func convertTxState(tx *types.MeshTransaction) spacemeshv2alpha1.TransactionState {
	switch tx.State {
	case types.MEMPOOL:
		return spacemeshv2alpha1.TransactionState_TRANSACTION_STATE_MEMPOOL
	case types.APPLIED:
		return spacemeshv2alpha1.TransactionState_TRANSACTION_STATE_PROCESSED
	default:
		return spacemeshv2alpha1.TransactionState_TRANSACTION_STATE_UNSPECIFIED
	}
}
