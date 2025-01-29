package v2alpha1

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-scale"
	"go.uber.org/zap"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/templates/mint"
	"github.com/spacemeshos/go-spacemesh/vm/templates/multisig"
	tokenwallet "github.com/spacemeshos/go-spacemesh/vm/templates/token_wallet"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

const (
	Transaction       = "transaction_v2alpha1"
	TransactionStream = "transaction_stream_v2alpha1"
)

// transactionConState is an API to validate transaction.
type transactionConState interface {
	Validation(raw types.RawTx) system.ValidationRequest
	HasEvicted(tid types.TransactionID) (bool, error)
}

// transactionSyncer is an API to get sync status.
type transactionSyncer interface {
	IsSynced(context.Context) bool
}

// transactionValidator is the API to validate and cache transactions.
type transactionValidator interface {
	VerifyAndCacheTx(context.Context, []byte) error
}

func NewTransactionStreamService() *TransactionStreamService {
	return &TransactionStreamService{}
}

type TransactionStreamService struct{}

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
		rst = append(rst, s.toTx(ctx, tx, result, request.IncludeResult, request.IncludeState))
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
	if request.Verify {
		if err := req.Verify(); err != nil {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("tx failed verification: %v", err))
		}
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
		contents, txType, err := toTxContents(raw.Raw, header)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		t.Type = txType
		t.Contents = contents
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
	ctxzap.Info(ctx, "successfully submitted transaction",
		zap.Stringer("tx_id", raw.ID),
	)
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

	if filter.GetAddress() != "" {
		addr, err := types.StringToAddress(filter.GetAddress())
		if err != nil {
			return builder.Operations{}, err
		}
		ops.Filter = append(ops.Filter, builder.Op{
			Value:       addr.Bytes(),
			CustomQuery: "id IN (SELECT tid FROM transactions_results_addresses WHERE address = ?1)",
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

	ops.Modifiers = append(ops.Modifiers, builder.Modifier{
		Key:   builder.OrderBy,
		Value: fmt.Sprintf("layer %s, id", filter.SortOrder.String()),
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

func (s *TransactionService) toTx(
	ctx context.Context,
	tx *types.MeshTransaction,
	result *types.TransactionResult,
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
		// t.Method = uint32(tx.Method)
		t.Nonce = &spacemeshv2alpha1.Nonce{Counter: tx.Nonce}
		t.MaxGas = tx.MaxGas
		t.GasPrice = tx.GasPrice
		t.MaxSpend = tx.MaxSpend

		contents, txType, err := toTxContents(tx.Raw, tx.TxHeader)
		if err != nil {
			return nil
		}
		t.Contents = contents
		t.Type = txType
	}

	if includeResult && result != nil {
		rst.TxResult = &spacemeshv2alpha1.TransactionResult{
			Status:      s.convertTxResult(result),
			Message:     result.Message,
			GasConsumed: result.Gas,
			Fee:         result.Fee,
			Block:       result.Block.Bytes(),
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
		rst.TxState = s.convertTxState(ctx, tx)
	}

	rst.Tx = t

	return rst
}

func (s *TransactionService) convertTxResult(
	result *types.TransactionResult,
) spacemeshv2alpha1.TransactionResult_Status {
	switch result.Status {
	case types.TransactionSuccess:
		return spacemeshv2alpha1.TransactionResult_TRANSACTION_STATUS_SUCCESS
	case types.TransactionFailure:
		return spacemeshv2alpha1.TransactionResult_TRANSACTION_STATUS_FAILURE
	default:
		return spacemeshv2alpha1.TransactionResult_TRANSACTION_STATUS_UNSPECIFIED
	}
}

func (s *TransactionService) convertTxState(
	ctx context.Context, tx *types.MeshTransaction,
) *spacemeshv2alpha1.TransactionState {
	switch tx.State {
	case types.MEMPOOL:
		state := spacemeshv2alpha1.TransactionState_TRANSACTION_STATE_MEMPOOL
		return &state
	case types.APPLIED:
		state := spacemeshv2alpha1.TransactionState_TRANSACTION_STATE_PROCESSED
		return &state
	default:
		evicted, err := s.conState.HasEvicted(tx.ID)
		if err != nil {
			ctxzap.Debug(ctx, "failed to check if tx is evicted",
				zap.String("tx_id", tx.ID.String()),
				zap.Error(err),
			)
			state := spacemeshv2alpha1.TransactionState_TRANSACTION_STATE_UNSPECIFIED
			return &state
		}
		if evicted {
			state := spacemeshv2alpha1.TransactionState_TRANSACTION_STATE_INEFFECTUAL
			return &state
		}
		state := spacemeshv2alpha1.TransactionState_TRANSACTION_STATE_UNSPECIFIED
		return &state
	}
}

func toTxContents(rawTx []byte, header *types.TxHeader) (
	*spacemeshv2alpha1.TransactionContents, spacemeshv2alpha1.Transaction_TransactionType, error,
) {
	res := &spacemeshv2alpha1.TransactionContents{}
	txType := spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_UNSPECIFIED

	var tx core.Tx
	_, err := tx.DecodeScale(scale.NewDecoder(bytes.NewReader(rawTx)))
	if err != nil {
		return nil, txType, fmt.Errorf("%w: decoding TX: %w", core.ErrMalformed, err)
	}

	var payload athcon.Payload
	err = gossamerScale.Unmarshal(tx.Payload, &payload)
	if err != nil {
		return nil, txType, fmt.Errorf("%w: tx payload: %w", core.ErrMalformed, err)
	}

	switch header.TemplateAddress {
	case wallet.TemplateAddress:
		txArgs, err := wallet.ParseArgs(payload)
		if err != nil {
			return nil, txType, fmt.Errorf("%w: decoding TX args: %w", core.ErrMalformed, err)
		}
		switch args := txArgs.(type) {
		case *wallet.SpawnArgs:
			res.Contents = &spacemeshv2alpha1.TransactionContents_SingleSigSpawn{
				SingleSigSpawn: &spacemeshv2alpha1.ContentsSingleSigSpawn{
					Pubkey: signing.NewPublicKey(args.Pubkey[:]).String(),
				},
			}
			txType = spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_SINGLE_SIG_SPAWN
		case *wallet.SpendArgs:
			res.Contents = &spacemeshv2alpha1.TransactionContents_Send{
				Send: &spacemeshv2alpha1.ContentsSend{
					Destination: args.To.String(),
					Amount:      args.Amount,
				},
			}
			txType = spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_SINGLE_SIG_SEND
		case *wallet.DeployArgs:
			res.Contents = &spacemeshv2alpha1.TransactionContents_Deploy{
				Deploy: &spacemeshv2alpha1.ContentsDeploy{
					Template: core.TemplateAddress(args.Code).String(),
				},
			}
			txType = spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_DEPLOY
		case *wallet.ProxyArgs:
			contents := &spacemeshv2alpha1.TransactionContents_Proxy{
				Proxy: &spacemeshv2alpha1.ContentsProxy{
					Destination: args.Destination.String(),
					Amount:      args.Amount,
				},
			}
			if args.Method != nil {
				contents.Proxy.Method = args.Method[:]
			}
			res.Contents = contents
			txType = spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_PROXY

		}
	case multisig.TemplateAddress:
		txArgs, err := multisig.ParseArgs(payload)
		if err != nil {
			return nil, txType, fmt.Errorf("%w: decoding TX args: %w", core.ErrMalformed, err)
		}
		switch args := txArgs.(type) {
		case *multisig.SpawnArguments:
			pubs := make([]string, 0, len(args.PublicKeys))
			for _, pub := range args.PublicKeys {
				pubs = append(pubs, pub.String())
			}
			res.Contents = &spacemeshv2alpha1.TransactionContents_MultiSigSpawn{
				MultiSigSpawn: &spacemeshv2alpha1.ContentsMultiSigSpawn{
					Required: uint32(args.Required),
					Pubkey:   pubs,
				},
			}
			txType = spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_MULTI_SIG_SPAWN
		case *multisig.SpendArguments:
			res.Contents = &spacemeshv2alpha1.TransactionContents_Send{
				Send: &spacemeshv2alpha1.ContentsSend{
					Destination: args.To.String(),
					Amount:      args.Amount,
				},
			}
			txType = spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_MULTI_SIG_SEND
		case *multisig.DeployArguments:
			res.Contents = &spacemeshv2alpha1.TransactionContents_Deploy{
				Deploy: &spacemeshv2alpha1.ContentsDeploy{
					Template: core.TemplateAddress(args.Code).String(),
				},
			}
			txType = spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_DEPLOY
		}
	case mint.TemplateAddress:
		txArgs, err := mint.ParseArgs(payload)
		if err != nil {
			return nil, txType, fmt.Errorf("%w: decoding TX args: %w", core.ErrMalformed, err)
		}
		switch args := txArgs.(type) {
		case *mint.SpawnArguments:
			res.Contents = &spacemeshv2alpha1.TransactionContents_MintSpawn{
				MintSpawn: &spacemeshv2alpha1.ContentsMintSpawn{
					Pubkey:    args.Owner.String(),
					MaxSupply: args.MaxSupply,
					Price:     args.MaxSupply,
				},
			}
			txType = spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_MINT_SPAWN
		}
	case tokenwallet.TemplateAddress:
		txArgs, err := tokenwallet.ParseArgs(payload)
		if err != nil {
			return nil, txType, fmt.Errorf("%w: decoding TX args: %w", core.ErrMalformed, err)
		}
		switch args := txArgs.(type) {
		case *tokenwallet.SpawnArguments:
			res.Contents = &spacemeshv2alpha1.TransactionContents_TokenWalletSpawn{
				TokenWalletSpawn: &spacemeshv2alpha1.ContentsTokenWalletSpawn{
					Pubkey:         args.Owner.String(),
					MintTemplate:   args.MintTemplate.String(),
					WalletTemplate: args.WalletTemplate.String(),
				},
			}
			txType = spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_TOKEN_WALLET_SPAWN
		case *tokenwallet.SendTokenArguments:
			res.Contents = &spacemeshv2alpha1.TransactionContents_TokenWalletSendToken{
				TokenWalletSendToken: &spacemeshv2alpha1.ContentsTokenWalletSendToken{
					Destination: args.To.String(),
					Amount:      args.Amount,
					TokenId:     args.TokenId.String(),
				},
			}
			txType = spacemeshv2alpha1.Transaction_TRANSACTION_TYPE_TOKEN_WALLET_SEND_TOKEN
		case *tokenwallet.SpendArguments:
			res.Contents = &spacemeshv2alpha1.TransactionContents_Send{
				Send: &spacemeshv2alpha1.ContentsSend{
					Destination: args.To.String(),
					Amount:      args.Amount,
				},
			}
		}
	}

	return res, txType, nil
}
