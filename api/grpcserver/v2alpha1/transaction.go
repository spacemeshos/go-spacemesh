package v2alpha1

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-scale"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vault"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vesting"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/system"
)

const (
	Transaction       = "transaction_v2alpha1"
	TransactionStream = "transaction_stream_v2alpha1"
)

// transactionConState is an API to validate transaction.
type transactionConState interface {
	Validation(raw types.RawTx) system.ValidationRequest
}

// transactionSyncer is an API to get sync status.
type transactionSyncer interface {
	IsSynced(context.Context) bool
}

// transactionValidator is the API to validate and cache transactions.
type transactionValidator interface {
	VerifyAndCacheTx(context.Context, []byte) error
}

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
	return status.Errorf(codes.Unimplemented, "this endpoint has not yet been implemented")
}

func (s *TransactionStreamService) String() string {
	return "TransactionStreamService"
}

func NewTransactionService(db sql.Executor, conState transactionConState,
	syncer transactionSyncer, validator transactionValidator,
	publisher pubsub.Publisher,
) *TransactionService {
	return &TransactionService{
		db:        db,
		conState:  conState,
		syncer:    syncer,
		validator: validator,
		publisher: publisher,
	}
}

type TransactionService struct {
	db        sql.Executor
	conState  transactionConState
	syncer    transactionSyncer
	validator transactionValidator
	publisher pubsub.Publisher // P2P Swarm
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
	switch {
	case request.Limit > 100:
		return nil, status.Error(codes.InvalidArgument, "limit is capped at 100")
	case request.Limit == 0:
		return nil, status.Error(codes.InvalidArgument, "limit must be set to <= 100")
	}

	ops, err := toTransactionOperations(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	rst := make([]*spacemeshv2alpha1.TransactionResponse, 0, request.Limit)
	if err := transactions.IterateTransactionsOps(s.db, ops, func(tx *types.MeshTransaction,
		result *types.TransactionResult,
	) bool {
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
	if len(request.Transaction) == 0 {
		return nil, status.Error(codes.InvalidArgument, "transaction is empty")
	}
	raw := types.NewRawTx(request.Transaction)
	req := s.conState.Validation(raw)
	header, err := req.Parse()
	if errors.Is(err, core.ErrNotSpawned) {
		return nil, status.Error(codes.NotFound, "account is not spawned")
	} else if errors.Is(err, core.ErrMalformed) {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	} else if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if request.Verify && !req.Verify() {
		return nil, status.Error(codes.InvalidArgument, "signature is invalid")
	}

	t := &spacemeshv2alpha1.Transaction{
		Raw: raw.Raw,
	}

	if header != nil {
		t.Principal = header.Principal.String()
		t.Template = header.TemplateAddress.String()
		t.Method = uint32(header.Method)
		t.Nonce = &spacemeshv2alpha1.Nonce{Counter: header.Nonce}
		t.MaxGas = header.MaxGas
		t.GasPrice = header.GasPrice
		t.MaxSpend = header.MaxSpend
		t.Contents = &spacemeshv2alpha1.TransactionContents{}
	}

	return &spacemeshv2alpha1.ParseTransactionResponse{
		Tx: t,
	}, nil
}

func (s *TransactionService) SubmitTransaction(
	ctx context.Context,
	request *spacemeshv2alpha1.SubmitTransactionRequest,
) (*spacemeshv2alpha1.SubmitTransactionResponse, error) {
	if len(request.Transaction) == 0 {
		return nil, status.Error(codes.InvalidArgument, "transaction is empty")
	}

	if !s.syncer.IsSynced(ctx) {
		return nil, status.Error(
			codes.FailedPrecondition,
			"Cannot submit transaction, node is not in sync yet, try again later",
		)
	}

	if err := s.validator.VerifyAndCacheTx(ctx, request.Transaction); err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Failed to verify transaction: %s", err.Error()))
	}

	if err := s.publisher.Publish(ctx, pubsub.TxProtocol, request.Transaction); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to publish transaction: %s", err.Error()))
	}

	raw := types.NewRawTx(request.Transaction)
	return &spacemeshv2alpha1.SubmitTransactionResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
		TxId:   raw.ID[:],
	}, nil
}

func (s *TransactionService) EstimateGas(
	ctx context.Context,
	request *spacemeshv2alpha1.EstimateGasRequest,
) (*spacemeshv2alpha1.EstimateGasResponse, error) {
	if len(request.Transaction) == 0 {
		return nil, status.Error(codes.InvalidArgument, "transaction is empty")
	}
	raw := types.NewRawTx(request.Transaction)
	req := s.conState.Validation(raw)
	// TODO: Fill signature if it's not present
	header, err := req.Parse()
	if errors.Is(err, core.ErrNotSpawned) {
		return nil, status.Error(codes.NotFound, "account is not spawned")
	} else if errors.Is(err, core.ErrMalformed) {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	} else if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &spacemeshv2alpha1.EstimateGasResponse{
		Status:            nil,
		RecommendedMaxGas: header.MaxGas,
	}, nil
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
			Field: builder.Principal,
			Token: builder.Eq,
			Value: addr.Bytes(),
		})
	}

	if len(filter.Txid) > 0 {
		ops.Filter = append(ops.Filter, builder.Op{
			Field: builder.Id,
			Token: builder.In,
			Value: filter.Txid,
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
			Field: builder.Layer,
			Token: builder.Lte,
			Value: int64(filter.GetEndLayer()),
		})
	}

	if len(ops.Filter) > 0 {
		ops.StartWith = "and"
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

func toTx(tx *types.MeshTransaction, result *types.TransactionResult,
	includeResult, includeState bool,
) *spacemeshv2alpha1.TransactionResponse {
	rst := &spacemeshv2alpha1.TransactionResponse{}

	t := &spacemeshv2alpha1.Transaction{
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

		txArgs, _ := decodeTxArgs(scale.NewDecoder(bytes.NewReader(tx.Raw)))
		switch tx.Method {
		case core.MethodSpawn:
			switch tx.TxHeader.TemplateAddress {
			case wallet.TemplateAddress:
				args := txArgs.(*wallet.SpawnArguments)
				t.Contents.Contents = &spacemeshv2alpha1.TransactionContents_SingleSigSpawn{
					SingleSigSpawn: &spacemeshv2alpha1.ContentsSingleSigSpawn{
						Pubkey: args.PublicKey.String(),
					},
				}
			case multisig.TemplateAddress:
				args := txArgs.(*multisig.SpawnArguments)
				contents := &spacemeshv2alpha1.TransactionContents_MultiSigSpawn{
					MultiSigSpawn: &spacemeshv2alpha1.ContentsMultiSigSpawn{
						Required: uint32(args.Required),
					},
				}
				contents.MultiSigSpawn.Pubkey = make([]string, len(args.PublicKeys))
				for i := range args.PublicKeys {
					contents.MultiSigSpawn.Pubkey[i] = args.PublicKeys[i].String()
				}
				t.Contents.Contents = contents
			}
		case core.MethodSpend:
			args := txArgs.(*wallet.SpendArguments)
			t.Contents.Contents = &spacemeshv2alpha1.TransactionContents_Send{
				Send: &spacemeshv2alpha1.ContentsSend{
					Destination: args.Destination.String(),
					Amount:      args.Amount,
				},
			}
		}
	}

	if includeResult {
		rst.TxResult = &spacemeshv2alpha1.TransactionResult{
			Status:      convertTxResult(result),
			Message:     result.Message,
			GasConsumed: result.Gas,
			Fee:         result.Fee,
			Block:       result.Block[:],
			Layer:       result.Layer.Uint32(),
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

	rst.Tx = t

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

// TODO: REJECTED, INSUFFICIENT_FUNDS, CONFLICTING, MESH.
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

func decodeTxArgs(decoder *scale.Decoder) (scale.Encodable, error) {
	reg := registry.New()
	wallet.Register(reg)
	multisig.Register(reg)
	vesting.Register(reg)
	vault.Register(reg)

	_, _, err := scale.DecodeCompact8(decoder)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to decode version %w", core.ErrMalformed, err)
	}

	var principal core.Address
	if _, err := principal.DecodeScale(decoder); err != nil {
		return nil, fmt.Errorf("%w failed to decode principal: %w", core.ErrMalformed, err)
	}

	method, _, err := scale.DecodeCompact8(decoder)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to decode method selector %w", core.ErrMalformed, err)
	}

	var templateAddress *core.Address
	var handler core.Handler
	if method == core.MethodSpawn {
		templateAddress = &core.Address{}
		if _, err := templateAddress.DecodeScale(decoder); err != nil {
			return nil, fmt.Errorf("%w failed to decode template address %w", core.ErrMalformed, err)
		}
	} else {
		templateAddress = &wallet.TemplateAddress
	}

	handler = reg.Get(*templateAddress)
	if handler == nil {
		return nil, fmt.Errorf("%w: unknown template %s", core.ErrMalformed, *templateAddress)
	}

	var p core.Payload
	if _, err = p.DecodeScale(decoder); err != nil {
		return nil, fmt.Errorf("%w: %w", core.ErrMalformed, err)
	}

	args := handler.Args(method)
	if args == nil {
		return nil, fmt.Errorf("%w: unknown method %s %d", core.ErrMalformed, *templateAddress, method)
	}
	if _, err := args.DecodeScale(decoder); err != nil {
		return nil, fmt.Errorf("%w failed to decode method arguments %w", core.ErrMalformed, err)
	}

	return args, nil
}
