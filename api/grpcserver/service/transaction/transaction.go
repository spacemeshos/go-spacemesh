package transaction

import (
	"bytes"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/api"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver/server"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/state"
)

// Transaction exposes transaction data, and a submit tx endpoint
type Transaction struct {
	Network api.NetworkAPI // P2P Swarm
	Mesh    api.TxAPI      // Mesh
	Mempool *state.TxMempool

	log log.Logger
}

var _ (server.API) = (*Transaction)(nil)

// Option type definds an callback that can set internal Transaction fields
type Option func(m *Transaction)

// WithLogger set's the underlying logger to a custom logger. By default the logger is NoOp
func WithLogger(log log.Logger) Option {
	return func(m *Transaction) {
		m.log = log
	}
}

// Register registers this service with a grpc server instance
func (t Transaction) Register(s *server.Server) {
	pb.RegisterTransactionServiceServer(s.GrpcServer, t)
}

// New creates a new grpc service using config data.
func New(net api.NetworkAPI, tx api.TxAPI, mempool *state.TxMempool, options ...Option) *Transaction {
	t := &Transaction{
		Network: net,
		Mesh:    tx,
		Mempool: mempool,
		log:     log.NewNop(),
	}
	for _, option := range options {
		option(t)
	}
	return t
}

// SubmitTransaction allows a new tx to be submitted
func (t Transaction) SubmitTransaction(ctx context.Context, in *pb.SubmitTransactionRequest) (*pb.SubmitTransactionResponse, error) {
	t.log.Info("GRPC TransactionService.SubmitTransaction")

	tx, err := types.BytesToTransaction(in.Transaction)
	if err != nil {
		t.log.Error("failed to deserialize tx, error: %v", err)
		return nil, status.Errorf(codes.InvalidArgument,
			"`Transaction` must contain a valid, serialized transaction")
	}
	t.log.Info("GRPC TransactionService.SubmitTransaction to address: %s (len: %v), "+
		"amount: %v, gaslimit: %v, fee: %v",
		tx.Recipient.String(), len(tx.Recipient), tx.Amount, tx.GasLimit, tx.Fee)
	if err := tx.CalcAndSetOrigin(); err != nil {
		t.log.Error("failed to calculate tx origin: %v", err)
		return nil, status.Errorf(codes.InvalidArgument,
			"`Transaction` must contain a valid, serialized transaction")
	}
	if !t.Mesh.AddressExists(tx.Origin()) {
		t.log.With().Error("tx origin address not found in global state",
			tx.ID(), log.String("origin", tx.Origin().Short()))
		return nil, status.Errorf(codes.InvalidArgument, "`Transaction` origin account not found")
	}
	if err := t.Mesh.ValidateNonceAndBalance(tx); err != nil {
		t.log.Error("tx failed nonce and balance check: %v", err)
		return nil, status.Errorf(codes.InvalidArgument, "`Transaction` incorrect counter or insufficient balance")
	}
	t.log.Info("GRPC TransactionService.SubmitTransaction BROADCAST tx address %x (len %v), gas limit %v, fee %v id %v nonce %v",
		tx.Recipient, len(tx.Recipient), tx.GasLimit, tx.Fee, tx.ID().ShortString(), tx.AccountNonce)
	go func() {
		if err := t.Network.Broadcast(state.IncomingTxProtocol, in.Transaction); err != nil {
			t.log.Error("error broadcasting incoming tx: %v", err)
		}
	}()

	return &pb.SubmitTransactionResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
		Txstate: &pb.TransactionState{
			Id:    &pb.TransactionId{Id: tx.ID().Bytes()},
			State: pb.TransactionState_TRANSACTION_STATE_MEMPOOL,
		},
	}, nil
}

// Get transaction and status for a given txid. It's not an error if we cannot find the tx,
// we just return all nils.
func (t Transaction) getTransactionAndStatus(txID types.TransactionID) (retTx *types.Transaction, state pb.TransactionState_TransactionState) {
	tx, err := t.Mesh.GetTransaction(txID) // have we seen this transaction in a block?
	retTx = tx
	if err != nil {
		tx, err = t.Mempool.Get(txID) // do we have it in the mempool?
		if err != nil {               // we don't know this transaction
			return
		}
		state = pb.TransactionState_TRANSACTION_STATE_MEMPOOL
		return
	}

	layer := t.Mesh.GetLayerApplied(txID)
	if layer != nil {
		state = pb.TransactionState_TRANSACTION_STATE_PROCESSED
	} else {
		nonce := t.Mesh.GetNonce(tx.Origin())
		if nonce > tx.AccountNonce {
			state = pb.TransactionState_TRANSACTION_STATE_REJECTED
		} else {
			state = pb.TransactionState_TRANSACTION_STATE_MESH
		}
	}
	return
}

// TransactionsState returns current tx data for one or more txs
func (t Transaction) TransactionsState(ctx context.Context, in *pb.TransactionsStateRequest) (*pb.TransactionsStateResponse, error) {
	t.log.Info("GRPC TransactionService.TransactionsState")

	if in.TransactionId == nil || len(in.TransactionId) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "`TransactionId` must include one or more transaction IDs")
	}

	res := &pb.TransactionsStateResponse{}
	for _, pbtxid := range in.TransactionId {
		// Convert the incoming txid into a known type
		txid := types.TransactionID{}
		copy(txid[:], pbtxid.Id)

		// Look up data for this tx. If it's unknown to us, status will be zero (unspecified).
		tx, state := t.getTransactionAndStatus(txid)
		res.TransactionsState = append(res.TransactionsState, &pb.TransactionState{
			Id:    pbtxid,
			State: state,
		})

		if in.IncludeTransactions {
			if tx != nil {
				res.Transactions = append(res.Transactions, Convert(tx))
			} else {
				// If the tx is unknown to us, add an empty placeholder
				res.Transactions = append(res.Transactions, &pb.Transaction{})
			}
		}
	}

	return res, nil
}

// STREAMS

// TransactionsStateStream exposes a stream of tx data
func (t Transaction) TransactionsStateStream(in *pb.TransactionsStateStreamRequest, stream pb.TransactionService_TransactionsStateStreamServer) error {
	t.log.Info("GRPC TransactionService.TransactionsStateStream")

	if in.TransactionId == nil || len(in.TransactionId) == 0 {
		return status.Errorf(codes.InvalidArgument, "`TransactionId` must include one or more transaction IDs")
	}

	// The tx channel tells us about newly received and newly created transactions
	channelTx := events.GetNewTxChannel()
	// The layer channel tells us about status updates
	channelLayer := events.GetLayerChannel()

	for {
		select {
		case tx, ok := <-channelTx:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				t.log.Info("tx channel closed, shutting down")
				return nil
			}

			// Filter
			for _, txid := range in.TransactionId {
				if bytes.Equal(tx.Transaction.ID().Bytes(), txid.Id) {
					// If the tx was just invalidated, we already know its state.
					// If not, read it from the database.
					var state pb.TransactionState_TransactionState
					if tx.Valid {
						_, state = t.getTransactionAndStatus(tx.Transaction.ID())
					} else {
						state = pb.TransactionState_TRANSACTION_STATE_CONFLICTING
					}

					res := &pb.TransactionsStateStreamResponse{
						TransactionState: &pb.TransactionState{
							Id:    txid,
							State: state,
						},
					}
					if in.IncludeTransactions {
						res.Transaction = Convert(tx.Transaction)
					}
					if err := stream.Send(res); err != nil {
						return err
					}

					// Don't match on any other transactions
					break
				}
			}
		case layer, ok := <-channelLayer:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				t.log.Info("layer channel closed, shutting down")
				return nil
			}
			// Filter for any matching transactions in the reported layer
			// TODO: this is inefficient and could be optimized!
			// See https://github.com/spacemeshos/go-spacemesh/issues/2076
			for _, b := range layer.Layer.Blocks() {
				for _, layerTxid := range b.TxIDs {
					for _, txid := range in.TransactionId {
						if bytes.Equal(layerTxid.Bytes(), txid.Id) {
							var state pb.TransactionState_TransactionState
							switch layer.Status {
							case events.LayerStatusTypeApproved:
								state = pb.TransactionState_TRANSACTION_STATE_MESH
							case events.LayerStatusTypeConfirmed:
								state = pb.TransactionState_TRANSACTION_STATE_PROCESSED
							default:
								state = pb.TransactionState_TRANSACTION_STATE_UNSPECIFIED
							}
							res := &pb.TransactionsStateStreamResponse{
								TransactionState: &pb.TransactionState{
									Id:    txid,
									State: state,
								},
							}
							if in.IncludeTransactions {
								tx, err := t.Mesh.GetTransaction(layerTxid)
								if err != nil {
									t.log.Error("could not find transaction %v from layer %v: %v", layerTxid, layer, err)
									return status.Errorf(codes.Internal, "error retrieving tx data")
								}

								res.Transaction = Convert(tx)
							}
							if err := stream.Send(res); err != nil {
								return err
							}

							// Don't match on any other transactions
							break
						}
					}
				}
			}
		case <-stream.Context().Done():
			t.log.Info("TransactionsStateStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}
