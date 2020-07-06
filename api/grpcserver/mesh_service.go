package grpcserver

import (
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MeshService is a grpc server providing the MeshService
type MeshService struct {
	Network          api.NetworkAPI // P2P Swarm
	Mesh             api.TxAPI      // Mesh
	Mempool          *miner.TxMempool
	GenTime          api.GenesisTimeAPI
	PeerCounter      api.PeerCounter
	Syncer           api.Syncer
	LayersPerEpoch   int
	NetworkID        int8
	LayerDurationSec int
	LayerAvgSize     int
	TxsPerBlock      int
}

// RegisterService registers this service with a grpc server instance
func (s MeshService) RegisterService(server *Server) {
	pb.RegisterMeshServiceServer(server.GrpcServer, s)
}

// NewMeshService creates a new grpc service using config data.
func NewMeshService(
	net api.NetworkAPI, tx api.TxAPI, mempool *miner.TxMempool, genTime api.GenesisTimeAPI,
	syncer api.Syncer, layersPerEpoch int, networkID int8, layerDurationSec int,
	layerAvgSize int, txsPerBlock int) *MeshService {
	return &MeshService{
		Network:          net,
		Mesh:             tx,
		Mempool:          mempool,
		GenTime:          genTime,
		PeerCounter:      peers.NewPeers(net, log.NewDefault("grpc_server.MeshService")),
		Syncer:           syncer,
		LayersPerEpoch:   layersPerEpoch,
		NetworkID:        networkID,
		LayerDurationSec: layerDurationSec,
		LayerAvgSize:     layerAvgSize,
		TxsPerBlock:      txsPerBlock,
	}
}

// GenesisTime returns the network genesis time as UNIX time
func (s MeshService) GenesisTime(ctx context.Context, in *pb.GenesisTimeRequest) (*pb.GenesisTimeResponse, error) {
	log.Info("GRPC MeshService.GenesisTime")
	return &pb.GenesisTimeResponse{Unixtime: &pb.SimpleInt{
		Value: uint64(s.GenTime.GetGenesisTime().Unix()),
	}}, nil
}

// CurrentLayer returns the current layer number
func (s MeshService) CurrentLayer(ctx context.Context, in *pb.CurrentLayerRequest) (*pb.CurrentLayerResponse, error) {
	log.Info("GRPC MeshService.CurrentLayer")
	return &pb.CurrentLayerResponse{Layernum: &pb.SimpleInt{
		Value: s.GenTime.GetCurrentLayer().Uint64(),
	}}, nil
}

// CurrentEpoch returns the current epoch number
func (s MeshService) CurrentEpoch(ctx context.Context, in *pb.CurrentEpochRequest) (*pb.CurrentEpochResponse, error) {
	log.Info("GRPC MeshService.CurrentEpoch")
	curLayer := s.GenTime.GetCurrentLayer()
	return &pb.CurrentEpochResponse{Epochnum: &pb.SimpleInt{
		Value: uint64(curLayer.GetEpoch(uint16(s.LayersPerEpoch))),
	}}, nil
}

// NetID returns the network ID
func (s MeshService) NetID(ctx context.Context, in *pb.NetIDRequest) (*pb.NetIDResponse, error) {
	log.Info("GRPC MeshService.NetId")
	return &pb.NetIDResponse{Netid: &pb.SimpleInt{
		Value: uint64(s.NetworkID),
	}}, nil
}

// EpochNumLayers returns the number of layers per epoch (a network parameter)
func (s MeshService) EpochNumLayers(ctx context.Context, in *pb.EpochNumLayersRequest) (*pb.EpochNumLayersResponse, error) {
	log.Info("GRPC MeshService.EpochNumLayers")
	return &pb.EpochNumLayersResponse{Numlayers: &pb.SimpleInt{
		Value: uint64(s.LayersPerEpoch),
	}}, nil
}

// LayerDuration returns the layer duration in seconds (a network parameter)
func (s MeshService) LayerDuration(ctx context.Context, in *pb.LayerDurationRequest) (*pb.LayerDurationResponse, error) {
	log.Info("GRPC MeshService.LayerDuration")
	return &pb.LayerDurationResponse{Duration: &pb.SimpleInt{
		Value: uint64(s.LayerDurationSec),
	}}, nil
}

// MaxTransactionsPerSecond returns the max number of tx per sec (a network parameter)
func (s MeshService) MaxTransactionsPerSecond(ctx context.Context, in *pb.MaxTransactionsPerSecondRequest) (*pb.MaxTransactionsPerSecondResponse, error) {
	log.Info("GRPC MeshService.MaxTransactionsPerSecond")
	return &pb.MaxTransactionsPerSecondResponse{Maxtxpersecond: &pb.SimpleInt{
		Value: uint64(s.TxsPerBlock * s.LayerAvgSize / s.LayerDurationSec),
	}}, nil
}

// QUERIES

// AccountMeshDataQuery returns account data
func (s MeshService) AccountMeshDataQuery(ctx context.Context, in *pb.AccountMeshDataQueryRequest) (*pb.AccountMeshDataQueryResponse, error) {
	log.Info("GRPC MeshService.AccountMeshDataQuery")

	// This is the latest layer we're aware of
	//latestLayer := s.Mesh.LatestLayer()
	startLayer := types.LayerID(in.MinLayer)

	if startLayer > s.Mesh.LatestLayer() {
		return nil, status.Errorf(codes.InvalidArgument, "`LatestLayer` must be less than or equal to latest layer")
	}
	if in.Filter == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Filter` must be provided")
	}
	if in.Filter.AccountId == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Filter.AccountId` must be provided")
	}

	// Gather transaction data
	addr := types.BytesToAddress(in.Filter.AccountId.Address)
	res := &pb.AccountMeshDataQueryResponse{}
	if in.Filter.AccountMeshDataFlags&uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS) != 0 {
		meshTxIds := s.getTxIdsFromMesh(startLayer, addr)
		mempoolTxIds := s.Mempool.GetTxIdsByAddress(addr)

		// Look up full data for all unique txids
		txs, missing := s.Mesh.GetTransactions(append(meshTxIds, mempoolTxIds...))

		// TODO: Do we ever expect txs to be missing here?
		// E.g., if this node has not synced/received them yet.
		if len(missing) != 0 {
			log.Error("could not find transactions %v", missing)
			return nil, status.Errorf(codes.Internal, "error retrieving tx data")
		}
		for _, t := range txs {
			res.Data = append(res.Data, &pb.AccountMeshData{
				DataItem: &pb.AccountMeshData_Transaction{
					Transaction: convertTransaction(t),
				},
			})
		}
	}

	// Gather activation data
	if in.Filter.AccountMeshDataFlags&uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS) != 0 {
		// We have no way to look up activations by coinbase so we have no choice
		// but to read all of them.
		// TODO: index activations by layer (and maybe by coinbase)
		var activations []types.ATXID
		for l := startLayer; l <= s.Mesh.LatestLayer(); l++ {
			layer, err := s.Mesh.GetLayer(l)
			if layer == nil || err != nil {
				return nil, status.Errorf(codes.Internal, "error retrieving layer data")
			}

			for _, b := range layer.Blocks() {
				activations = append(activations, b.ATXIDs...)
			}
		}

		// Look up full data
		atxs, matxs := s.Mesh.GetATXs(activations)
		if len(matxs) != 0 {
			log.Error("could not find activations %v", matxs)
			return nil, status.Errorf(codes.Internal, "error retrieving activations data")
		}
		for _, atx := range atxs {
			// Filter here, now that we have full data
			if atx.Coinbase == addr {
				pbatx, err := convertActivation(atx)
				if err != nil {
					return nil, status.Errorf(codes.Internal, "error serializing activation data")
				}
				res.Data = append(res.Data, &pb.AccountMeshData{
					DataItem: &pb.AccountMeshData_Activation{
						Activation: pbatx,
					},
				})
			}
		}
	}

	res.TotalResults = uint32(len(res.Data))
	return res, nil
}

func (s MeshService) getTxIdsFromMesh(minLayer types.LayerID, addr types.Address) []types.TransactionID {
	var txIDs []types.TransactionID
	for layerID := minLayer; layerID < s.Mesh.LatestLayer(); layerID++ {
		destTxIDs := s.Mesh.GetTransactionsByDestination(layerID, addr)
		txIDs = append(txIDs, destTxIDs...)
		originTxIds := s.Mesh.GetTransactionsByOrigin(layerID, addr)
		txIDs = append(txIDs, originTxIds...)
	}
	return txIDs
}

func convertTransaction(t *types.Transaction) *pb.Transaction {
	return &pb.Transaction{
		Id: &pb.TransactionId{Id: t.ID().Bytes()},
		Data: &pb.Transaction_CoinTransfer{
			CoinTransfer: &pb.CoinTransferTransaction{
				Receiver: &pb.AccountId{Address: t.Recipient.Bytes()},
			},
		},
		Sender: &pb.AccountId{Address: t.Origin().Bytes()},
		GasOffered: &pb.GasOffered{
			// TODO: Check this math! Does fee == price?
			GasPrice:    t.Fee,
			GasProvided: t.GasLimit,
		},
		Amount:  &pb.Amount{Value: t.Amount},
		Counter: t.AccountNonce,
		Signature: &pb.Signature{
			// TODO: confirm default signature scheme
			Scheme:    pb.Signature_SCHEME_ED25519_PLUS_PLUS,
			Signature: t.Signature[:],
			PublicKey: t.Origin().Bytes(),
		},
	}
}

func convertActivation(a *types.ActivationTx) (*pb.Activation, error) {
	// TODO: It's suboptimal to have to serialize every atx to get its
	// size but size is not stored as an attribute.
	data, err := a.InnerBytes()
	if err != nil {
		return nil, err
	}
	return &pb.Activation{
		Id:             &pb.ActivationId{Id: a.ID().Bytes()},
		Layer:          a.PubLayerID.Uint64(),
		SmesherId:      &pb.SmesherId{Id: a.NodeID.ToBytes()},
		Coinbase:       &pb.AccountId{Address: a.Coinbase.Bytes()},
		PrevAtx:        &pb.ActivationId{Id: a.PrevATXID.Bytes()},
		CommitmentSize: uint64(len(data)),
	}, nil
}

// LayersQuery returns all mesh data, layer by layer
func (s MeshService) LayersQuery(ctx context.Context, in *pb.LayersQueryRequest) (*pb.LayersQueryResponse, error) {
	log.Info("GRPC MeshService.LayersQuery")

	// last validated layer
	// TODO: Do we need to distinguish between layers that have passed tortoise,
	// passed hare, been applied to state, etc.? For now eveything is either
	// unspecified, or fully confirmed.
	// see https://github.com/spacemeshos/api/issues/89
	//lastValidLayer := s.Mesh.ProcessedLayer()
	lastValidLayer := s.Mesh.LatestLayerInState()

	layers := []*pb.Layer{}
	for l := uint64(in.StartLayer); l <= uint64(in.EndLayer); l++ {
		layerStatus := pb.Layer_LAYER_STATUS_UNSPECIFIED
		if l <= lastValidLayer.Uint64() {
			layerStatus = pb.Layer_LAYER_STATUS_CONFIRMED
		}

		layer, err := s.Mesh.GetLayer(types.LayerID(l))
		// TODO: Be careful with how we handle missing layers here.
		// A layer that's newer than the currentLayer (defined above)
		// is clearly an input error. A missing layer that's older than
		// lastValidLayer is clearly an internal error. A missing layer
		// between these two is a gray area: do we define this as an
		// internal or an input error? For now, all missing layers produce
		// internal errors.
		if layer == nil || err != nil {
			return nil, status.Errorf(codes.Internal, "error retrieving layer data")
		}

		// Load all block data
		var blocks []*pb.Block

		// Save activations too
		var activations []types.ATXID

		for _, b := range layer.Blocks() {
			txs, missing := s.Mesh.GetTransactions(b.TxIDs)
			// TODO: Do we ever expect txs to be missing here?
			// E.g., if this node has not synced/received them yet.
			if len(missing) != 0 {
				log.Error("could not find transactions %v from layer %v", missing, l)
				return nil, status.Errorf(codes.Internal, "error retrieving tx data")
			}

			activations = append(activations, b.ATXIDs...)

			var pbTxs []*pb.Transaction
			for _, t := range txs {
				if t == nil {
					log.Error("got empty transaction data from layer %v", l)
					return nil, status.Errorf(codes.Internal, "error retrieving tx data")
				}
				pbTxs = append(pbTxs, convertTransaction(t))
			}
			blocks = append(blocks, &pb.Block{
				Id:           b.ID().Bytes(),
				Transactions: pbTxs,
			})
		}

		// Extract ATX data from block data
		var pbActivations []*pb.Activation

		// Add unique ATXIDs
		atxs, matxs := s.Mesh.GetATXs(activations)
		if len(matxs) != 0 {
			log.Error("could not find activations %v from layer %v", matxs, l)
			return nil, status.Errorf(codes.Internal, "error retrieving activations data")
		}
		for _, atx := range atxs {
			pbatx, err := convertActivation(atx)
			if err != nil {
				log.Error("error serializing activation data: ", err)
				return nil, status.Errorf(codes.Internal, "error serializing activation data")
			}
			pbActivations = append(pbActivations, pbatx)
		}

		// TODO: We currently have no way to get state root for any
		// layer but the latest layer applied to state (which is
		// anyway not exposed by Mesh). The logic lives in
		// Mesh.txProcessor.GetStateRoot().
		// Tracked in https://github.com/spacemeshos/api/issues/90.
		layers = append(layers, &pb.Layer{
			Number:        l,
			Status:        layerStatus,
			Hash:          layer.Hash().Bytes(), // do we need this?
			Blocks:        blocks,
			Activations:   pbActivations,
			RootStateHash: nil, // do we need this?
		})
	}
	return &pb.LayersQueryResponse{Layer: layers}, nil
}

// STREAMS

// AccountMeshDataStream returns a stream of transactions and activations for an account
func (s MeshService) AccountMeshDataStream(in *pb.AccountMeshDataStreamRequest, stream pb.MeshService_AccountMeshDataStreamServer) error {
	log.Info("GRPC MeshService.AccountMeshDataStream")

	if in.Filter == nil {
		return status.Errorf(codes.InvalidArgument, "`Filter` must be provided")
	}
	if in.Filter.AccountId == nil {
		return status.Errorf(codes.InvalidArgument, "`Filter.AccountId` must be provided")
	}
	addr := types.BytesToAddress(in.Filter.AccountId.Address)

	// Read the filter flags
	filterTx := in.Filter.AccountMeshDataFlags&uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS) != 0
	filterActivations := in.Filter.AccountMeshDataFlags&uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS) != 0
	if !filterTx && !filterActivations {
		return status.Errorf(codes.InvalidArgument, "`Filter.AccountMeshDataFlags` must set at least one bitfield")
	}

	// Subscribe to the stream of transactions and activations
	var (
		txStream          chan *types.Transaction
		activationsStream chan *types.ActivationTx
	)
	if filterTx {
		txStream = events.GetNewTxStream()
	}
	if filterActivations {
		activationsStream = events.GetActivationStream()
	}

	for {
		select {
		case activation, ok := <-activationsStream:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("ActivationStream closed, shutting down")
				return nil
			}
			// Apply address filter
			if activation.Coinbase == addr {
				pbActivation, err := convertActivation(activation)
				if err != nil {
					log.Error("error serializing activation data: ", err)
					return status.Errorf(codes.Internal, "error serializing activation data")
				}
				if err := stream.Send(&pb.AccountMeshDataStreamResponse{
					Data: &pb.AccountMeshData{
						DataItem: &pb.AccountMeshData_Activation{
							Activation: pbActivation,
						},
					},
				}); err != nil {
					return err
				}
			}
		case tx, ok := <-txStream:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				log.Info("NewTxStream closed, shutting down")
				return nil
			}
			if err := stream.Send(&pb.AccountMeshDataStreamResponse{
				Data: &pb.AccountMeshData{
					DataItem: &pb.AccountMeshData_Transaction{
						Transaction: convertTransaction(tx),
					},
				},
			}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			log.Info("AccountMeshDataStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that MeshService needs to shut down?
	}
}

// LayerStream returns a stream of all mesh data per layer
func (s MeshService) LayerStream(in *pb.LayerStreamRequest, stream pb.MeshService_LayerStreamServer) error {
	log.Info("GRPC MeshService.LayerStream")
	return nil
}
