package grpcserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

// TransactionService exposes transaction data, and a submit tx endpoint.
type TransactionService struct {
	db        *sql.Database
	publisher api.Publisher // P2P Swarm
	mesh      api.MeshAPI   // Mesh
	conState  api.ConservativeState
	syncer    api.Syncer
	txHandler api.TxValidator
}

// RegisterService registers this service with a grpc server instance.
func (s TransactionService) RegisterService(server *Server) {
	pb.RegisterTransactionServiceServer(server.GrpcServer, s)
}

// NewTransactionService creates a new grpc service using config data.
func NewTransactionService(
	db *sql.Database,
	publisher api.Publisher,
	msh api.MeshAPI,
	conState api.ConservativeState,
	syncer api.Syncer,
	txHandler api.TxValidator,
) *TransactionService {
	return &TransactionService{
		db:        db,
		publisher: publisher,
		mesh:      msh,
		conState:  conState,
		syncer:    syncer,
		txHandler: txHandler,
	}
}

// SubmitTransaction allows a new tx to be submitted.
func (s TransactionService) SubmitTransaction(ctx context.Context, in *pb.SubmitTransactionRequest) (*pb.SubmitTransactionResponse, error) {
	log.Info("GRPC TransactionService.SubmitTransaction")

	if len(in.Transaction) == 0 {
		return nil, status.Error(codes.InvalidArgument, "`Transaction` payload empty")
	}

	if !s.syncer.IsSynced(ctx) {
		return nil, status.Error(codes.FailedPrecondition, "Cannot submit transaction, node is not in sync yet, try again later")
	}

	if err := s.txHandler.VerifyAndCacheTx(ctx, in.Transaction); err != nil {
		return nil, status.Error(codes.InvalidArgument, "Failed to verify transaction")
	}

	if err := s.publisher.Publish(ctx, pubsub.TxProtocol, in.Transaction); err != nil {
		return nil, status.Error(codes.Internal, "Failed to publish transaction")
	}

	raw := types.NewRawTx(in.Transaction)
	return &pb.SubmitTransactionResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
		Txstate: &pb.TransactionState{
			Id:    &pb.TransactionId{Id: raw.ID[:]},
			State: pb.TransactionState_TRANSACTION_STATE_MEMPOOL,
		},
	}, nil
}

// Get transaction and status for a given txid. It's not an error if we cannot find the tx,
// we just return all nils.
func (s TransactionService) getTransactionAndStatus(txID types.TransactionID) (*types.Transaction, pb.TransactionState_TransactionState) {
	var state pb.TransactionState_TransactionState
	tx, err := s.conState.GetMeshTransaction(txID)
	if err != nil {
		return nil, state
	}
	switch tx.State {
	case types.MEMPOOL:
		state = pb.TransactionState_TRANSACTION_STATE_MEMPOOL
	case types.APPLIED:
		state = pb.TransactionState_TRANSACTION_STATE_PROCESSED
	default:
		state = pb.TransactionState_TRANSACTION_STATE_UNSPECIFIED
	}
	return &tx.Transaction, state
}

// TransactionsState returns current tx data for one or more txs.
func (s TransactionService) TransactionsState(_ context.Context, in *pb.TransactionsStateRequest) (*pb.TransactionsStateResponse, error) {
	log.Info("GRPC TransactionService.TransactionsState")

	if in.TransactionId == nil || len(in.TransactionId) == 0 {
		return nil, status.Error(codes.InvalidArgument, "`TransactionId` must include one or more transaction IDs")
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

// TransactionsStateStream exposes a stream of tx data.
func (s TransactionService) TransactionsStateStream(in *pb.TransactionsStateStreamRequest, stream pb.TransactionService_TransactionsStateStreamServer) error {
	log.Info("GRPC TransactionService.TransactionsStateStream")

	if in.TransactionId == nil || len(in.TransactionId) == 0 {
		return status.Error(codes.InvalidArgument, "`TransactionId` must include one or more transaction IDs")
	}

	// The tx channel tells us about newly received and newly created transactions
	// The layer channel tells us about status updates
	var (
		txCh                    <-chan events.Transaction
		layerCh                 <-chan events.LayerUpdate
		txBufFull, layerBufFull <-chan struct{}
	)

	if txsSubscription := events.SubscribeTxs(); txsSubscription != nil {
		txCh, txBufFull = consumeEvents[events.Transaction](stream.Context(), txsSubscription)
	}

	if layersSubscription := events.SubscribeLayers(); layersSubscription != nil {
		layerCh, layerBufFull = consumeEvents[events.LayerUpdate](stream.Context(), layersSubscription)
	}

	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return status.Errorf(codes.Unavailable, "can't send header")
	}

	for {
		select {
		case <-txBufFull:
			log.Info("tx buffer is full, shutting down")
			return status.Error(codes.Canceled, errTxBufferFull)
		case <-layerBufFull:
			log.Info("layer buffer is full, shutting down")
			return status.Error(codes.Canceled, errLayerBufferFull)
		case tx := <-txCh:
			// Filter
			for _, txid := range in.TransactionId {
				if bytes.Equal(tx.Transaction.ID.Bytes(), txid.Id) {
					// If the tx was just invalidated, we already know its state.
					// If not, read it from the database.
					var txstate pb.TransactionState_TransactionState
					if tx.Valid {
						_, txstate = s.getTransactionAndStatus(tx.Transaction.ID)
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
						return fmt.Errorf("send stream: %w", err)
					}

					// Don't match on any other transactions
					break
				}
			}
		case layer := <-layerCh:
			// Transaction objects do not have an associated status. The status we assign them here is based on the
			// status of the layer (really, the block) they're contained in. Here, we receive updates to layer status.
			// In order to update tx status, we have to read every transaction in the layer.
			// TODO: this is inefficient, come up with a more optimal way of doing this
			// TODO: tx status should depend upon block status, not layer status

			// In order to read transactions, we first need to read layer blocks
			layerObj, err := s.mesh.GetLayer(layer.LayerID)
			if err != nil {
				log.With().Error("error reading layer data for updated layer", layer.LayerID, log.Err(err))
				return status.Error(codes.Internal, "error reading layer data")
			}

			// Filter for any matching transactions in the reported layer
			for _, b := range layerObj.Blocks() {
				blockTXIDSet := make(map[types.TransactionID]struct{})

				// create a set for the block transaction IDs
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
							tx, err := s.conState.GetMeshTransaction(txid)
							if err != nil {
								log.Error("could not find transaction %v from layer %v: %v", txid, layer, err)
								return status.Error(codes.Internal, "error retrieving tx data")
							}

							res.Transaction = convertTransaction(&tx.Transaction)
						}

						if err := stream.Send(res); err != nil {
							return fmt.Errorf("send stream: %w", err)
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

// StreamResults allows to query historical results and subscribe to live data using the same filter.
func (s TransactionService) StreamResults(in *pb.TransactionResultsRequest, stream pb.TransactionService_StreamResultsServer) error {
	var (
		filter    transactions.ResultsFilter
		sub       *events.BufferedSubscription[types.TransactionWithResult]
		err       error
		persisted types.LayerID
	)
	if len(in.Address) > 0 {
		addr, err := types.StringToAddress(in.Address)
		if err != nil {
			return fmt.Errorf("failed to parse in.Address `%s`: %w", in.Address, err)
		}
		filter.Address = &addr
	}
	if len(in.Id) > 0 {
		var id types.TransactionID
		copy(id[:], in.Id)
		filter.TID = &id
	}
	if in.Start > 0 {
		lid := types.LayerID(in.Start)
		filter.Start = &lid
	}
	if in.End > 0 {
		if in.Watch {
			return status.Error(codes.InvalidArgument, "watch stream should have an empty End argument")
		}
		lid := types.LayerID(in.End)
		filter.End = &lid
	}

	if in.Watch {
		sub, err = events.SubscribeMatched(resultsMatcher(filter).match)
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		defer sub.Close()
		if err := stream.SendHeader(metadata.MD{}); err != nil {
			return status.Errorf(codes.Unavailable, "can't send header")
		}
	}

	var ierr error
	err = transactions.IterateResults(s.db, filter, func(rst *types.TransactionWithResult) bool {
		if rst.Layer.After(persisted) {
			persisted = rst.Layer
		}
		ierr = stream.Send(castResult(rst))
		return ierr == nil
	})
	if err == nil {
		err = ierr
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return status.Error(codes.Internal, err.Error())
	}
	if sub == nil {
		return nil
	}
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-sub.Full():
			return status.Error(codes.Canceled, "buffer overflow")
		case rst := <-sub.Out():
			if !rst.Layer.After(persisted) {
				break
			}
			if err := stream.Send(castResult(&rst)); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return status.Error(codes.Internal, err.Error())
			}
		}
	}
}

func castResult(rst *types.TransactionWithResult) *pb.TransactionResult {
	casted := &pb.TransactionResult{
		Tx:          convertTransaction(&rst.Transaction),
		Status:      pb.TransactionResult_Status(rst.Status),
		Message:     rst.Message,
		GasConsumed: rst.Gas,
		Fee:         rst.Fee,
		Block:       rst.Block[:],
		Layer:       rst.Layer.Uint32(),
	}
	if len(rst.Addresses) > 0 {
		casted.TouchedAddresses = make([]string, len(rst.Addresses))
		for i := range rst.Addresses {
			casted.TouchedAddresses[i] = rst.Addresses[i].String()
		}
	}
	return casted
}

type resultsMatcher transactions.ResultsFilter

func (m resultsMatcher) match(rst *types.TransactionWithResult) bool {
	if m.Address != nil {
		found := false
		for i := range rst.Addresses {
			if rst.Addresses[i] == *m.Address {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if m.Start != nil {
		if rst.Layer.Before(*m.Start) {
			return false
		}
	}
	if m.End != nil {
		if rst.Layer.After(*m.End) {
			return false
		}
	}
	if m.TID != nil {
		if rst.ID != *m.TID {
			return false
		}
	}
	return true
}
