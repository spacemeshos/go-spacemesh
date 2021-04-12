package grpcserver

import (
	"bytes"
	"context"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/state"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TransactionService exposes transaction data, and a submit tx endpoint
type TransactionService struct {
	Network api.NetworkAPI // P2P Swarm
	Mesh    api.TxAPI      // Mesh
	Mempool api.MempoolAPI
	syncer  api.Syncer
}

// RegisterService registers this service with a grpc server instance
func (s TransactionService) RegisterService(server *Server) {
	pb.RegisterTransactionServiceServer(server.GrpcServer, s)
}

// NewTransactionService creates a new grpc service using config data.
func NewTransactionService(
	net api.NetworkAPI,
	tx api.TxAPI,
	mempool api.MempoolAPI,
	syncer api.Syncer,
) *TransactionService {
	return &TransactionService{
		Network: net,
		Mesh:    tx,
		Mempool: mempool,
		syncer:  syncer,
	}
}

// SubmitTransaction allows a new tx to be submitted
func (s TransactionService) SubmitTransaction(ctx context.Context, in *pb.SubmitTransactionRequest) (*pb.SubmitTransactionResponse, error) {
	log.Info("GRPC TransactionService.SubmitTransaction")

	if len(in.Transaction) == 0 {
		return nil, status.Error(codes.InvalidArgument, "`Transaction` payload empty")
	}

	if !s.syncer.IsSynced(ctx) {
		return nil, status.Error(codes.FailedPrecondition,
			"Cannot submit transaction, node is not in sync yet, try again later")
	}

	// Note: The TransactionProcessor performs these same checks, so there is some duplicated logic here.
	// See https://github.com/spacemeshos/go-spacemesh/issues/2162

	tx, err := types.BytesToTransaction(in.Transaction)
	if err != nil {
		log.Error("failed to deserialize tx, error: %v", err)
		return nil, status.Error(codes.InvalidArgument,
			"`Transaction` must contain a valid, serialized transaction")
	}
	if err := tx.CalcAndSetOrigin(); err != nil {
		log.Error("failed to calculate tx origin: %v", err)
		return nil, status.Error(codes.InvalidArgument,
			"`Transaction` must contain a valid, serialized transaction")
	}
	if !s.Mesh.AddressExists(tx.Origin()) {
		log.With().Error("tx origin address not found in global state",
			tx.ID(), log.String("origin", tx.Origin().Short()))
		return nil, status.Error(codes.InvalidArgument, "`Transaction` origin account not found")
	}
	if err := s.Mesh.ValidateNonceAndBalance(tx); err != nil {
		log.Error("tx failed nonce and balance check: %v", err)
		return nil, status.Error(codes.InvalidArgument, "`Transaction` incorrect counter or insufficient balance")
	}
	log.Info("GRPC TransactionService.SubmitTransaction BROADCAST tx address: %x (len: %v), amount: %v, gas limit: %v, fee: %v, id: %v, nonce: %v",
		tx.Recipient, len(tx.Recipient), tx.Amount, tx.GasLimit, tx.Fee, tx.ID().ShortString(), tx.AccountNonce)
	go func() {
		if err := s.Network.Broadcast(ctx, state.IncomingTxProtocol, in.Transaction); err != nil {
			log.Error("error broadcasting incoming tx: %v", err)
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
func (s TransactionService) getTransactionAndStatus(txID types.TransactionID) (retTx *types.Transaction, state pb.TransactionState_TransactionState) {
	tx, err := s.Mesh.GetTransaction(txID) // have we seen this transaction in a block?
	retTx = tx
	if err != nil {
		tx, err = s.Mempool.Get(txID) // do we have it in the mempool?
		if err != nil {               // we don't know this transaction
			return
		}
		state = pb.TransactionState_TRANSACTION_STATE_MEMPOOL
		return
	}

	layer := s.Mesh.GetLayerApplied(txID)
	if layer != nil {
		state = pb.TransactionState_TRANSACTION_STATE_PROCESSED
	} else {
		nonce := s.Mesh.GetNonce(tx.Origin())
		if nonce > tx.AccountNonce {
			state = pb.TransactionState_TRANSACTION_STATE_REJECTED
		} else {
			state = pb.TransactionState_TRANSACTION_STATE_MESH
		}
	}
	return
}

// TransactionsState returns current tx data for one or more txs
func (s TransactionService) TransactionsState(_ context.Context, in *pb.TransactionsStateRequest) (*pb.TransactionsStateResponse, error) {
	log.Info("GRPC TransactionService.TransactionsState")

	if in.TransactionId == nil || len(in.TransactionId) == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"`TransactionId` must include one or more transaction IDs")
	}

	res := &pb.TransactionsStateResponse{}
	for _, pbtxid := range in.TransactionId {
		// Convert the incoming txid into a known type
		txid := types.TransactionID{}
		copy(txid[:], pbtxid.Id)

		// Look up data for this tx. If it's unknown to us, status will be zero (unspecified).
		tx, txstate := s.getTransactionAndStatus(txid)
		res.TransactionsState = append(res.TransactionsState, &pb.TransactionState{
			Id:    pbtxid,
			State: txstate,
		})

		if in.IncludeTransactions {
			if tx != nil {
				res.Transactions = append(res.Transactions, convertTransaction(tx))
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
func (s TransactionService) TransactionsStateStream(in *pb.TransactionsStateStreamRequest, stream pb.TransactionService_TransactionsStateStreamServer) error {
	log.Info("GRPC TransactionService.TransactionsStateStream")

	if in.TransactionId == nil || len(in.TransactionId) == 0 {
		return status.Error(codes.InvalidArgument, "`TransactionId` must include one or more transaction IDs")
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
				log.Info("tx channel closed, shutting down")
				return nil
			}

			// Filter
			for _, txid := range in.TransactionId {
				if bytes.Equal(tx.Transaction.ID().Bytes(), txid.Id) {
					// If the tx was just invalidated, we already know its state.
					// If not, read it from the database.
					var txstate pb.TransactionState_TransactionState
					if tx.Valid {
						_, txstate = s.getTransactionAndStatus(tx.Transaction.ID())
					} else {
						txstate = pb.TransactionState_TRANSACTION_STATE_CONFLICTING
					}

					res := &pb.TransactionsStateStreamResponse{
						TransactionState: &pb.TransactionState{
							Id:    txid,
							State: txstate,
						},
					}
					if in.IncludeTransactions {
						res.Transaction = convertTransaction(tx.Transaction)
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
				log.Info("layer channel closed, shutting down")
				return nil
			}
			// Filter for any matching transactions in the reported layer
			for _, b := range layer.Layer.Blocks() {
				blockTXIDSet := make(map[types.TransactionID]struct{})

				//create a set for the block transaction IDs
				for _, txid := range b.TxIDs {
					blockTXIDSet[txid] = struct{}{}
				}

				var txstate pb.TransactionState_TransactionState
				switch layer.Status {
				case events.LayerStatusTypeApproved:
					txstate = pb.TransactionState_TRANSACTION_STATE_MESH
				case events.LayerStatusTypeConfirmed:
					txstate = pb.TransactionState_TRANSACTION_STATE_PROCESSED
				default:
					txstate = pb.TransactionState_TRANSACTION_STATE_UNSPECIFIED
				}

				for _, inputTxID := range in.TransactionId {
					// Since the txid coming in from the API does not have a fixed length (see
					// https://github.com/spacemeshos/api/issues/130), we need to convert it from a slice to a fixed
					// size array before we can convert it into a TransactionID object. We don't need to worry about
					// error handling, since copy intelligently copies only what it can. If the resulting TransactionID
					// is invalid, an error will be thrown below.
					var arrayID [32]byte
					copy(arrayID[:], inputTxID.Id[:])
					txid := types.TransactionID(arrayID)
					// if there is an ID corresponding to inputTxID in the block
					if _, exists := blockTXIDSet[txid]; exists {
						res := &pb.TransactionsStateStreamResponse{
							TransactionState: &pb.TransactionState{
								Id:    inputTxID,
								State: txstate,
							},
						}
						if in.IncludeTransactions {
							tx, err := s.Mesh.GetTransaction(txid)
							if err != nil {
								log.Error("could not find transaction %v from layer %v: %v", txid, layer, err)
								return status.Error(codes.Internal, "error retrieving tx data")
							}

							res.Transaction = convertTransaction(tx)
						}
						if err := stream.Send(res); err != nil {
							return err
						}
					}
				}
			}
		case <-stream.Context().Done():
			log.Info("TransactionsStateStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}
