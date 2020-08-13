package mesh

import (
	"fmt"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver/server"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver/service/transaction"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/state"
)

// Mesh exposes mesh data such as accounts, blocks, and transactions
type Mesh struct {
	Mesh             api.TxAPI // Mesh
	Mempool          *state.TxMempool
	GenTime          api.GenesisTimeAPI
	LayersPerEpoch   int
	NetworkID        int8
	LayerDurationSec int
	LayerAvgSize     int
	TxsPerBlock      int

	log log.Logger
}

var _ server.API = (*Mesh)(nil)

// Option type definds an callback that can set internal Mesh fields
type Option func(m *Mesh)

// WithLogger set's the underlying logger to a custom logger. By default the logger is NoOp
func WithLogger(log log.Logger) Option {
	return func(m *Mesh) {
		m.log = log
	}
}

// Register registers this service with a grpc server instance
func (m Mesh) Register(s *server.Server) {
	pb.RegisterMeshServiceServer(s.GrpcServer, &m)
}

// New creates a new service using config data
func New(
	tx api.TxAPI,
	mempool *state.TxMempool,
	genTime api.GenesisTimeAPI,
	layersPerEpoch int,
	networkID int8,
	layerDurationSec int,
	layerAvgSize int,
	txsPerBlock int,
	options ...Option,
) *Mesh {
	m := &Mesh{
		Mesh:             tx,
		Mempool:          mempool,
		GenTime:          genTime,
		LayersPerEpoch:   layersPerEpoch,
		NetworkID:        networkID,
		LayerDurationSec: layerDurationSec,
		LayerAvgSize:     layerAvgSize,
		TxsPerBlock:      txsPerBlock,
		log:              log.NewNop(),
	}
	for _, option := range options {
		option(m)
	}
	return m
}

// GenesisTime returns the network genesis time as UNIX time
func (m Mesh) GenesisTime(ctx context.Context, in *pb.GenesisTimeRequest) (*pb.GenesisTimeResponse, error) {
	m.log.Info("GRPC MeshService.GenesisTime")
	return &pb.GenesisTimeResponse{
		Unixtime: &pb.SimpleInt{
			Value: uint64(m.GenTime.GetGenesisTime().Unix()),
		},
	}, nil
}

// CurrentLayer returns the current layer number
func (m Mesh) CurrentLayer(ctx context.Context, in *pb.CurrentLayerRequest) (*pb.CurrentLayerResponse, error) {
	m.log.Info("GRPC MeshService.CurrentLayer")
	return &pb.CurrentLayerResponse{
		Layernum: &pb.SimpleInt{
			Value: m.GenTime.GetCurrentLayer().Uint64(),
		},
	}, nil
}

// CurrentEpoch returns the current epoch number
func (m Mesh) CurrentEpoch(ctx context.Context, in *pb.CurrentEpochRequest) (*pb.CurrentEpochResponse, error) {
	m.log.Info("GRPC MeshService.CurrentEpoch")
	curLayer := m.GenTime.GetCurrentLayer()
	return &pb.CurrentEpochResponse{
		Epochnum: &pb.SimpleInt{
			Value: uint64(curLayer.GetEpoch()),
		},
	}, nil
}

// NetID returns the network ID
func (m Mesh) NetID(ctx context.Context, in *pb.NetIDRequest) (*pb.NetIDResponse, error) {
	m.log.Info("GRPC MeshService.NetId")
	return &pb.NetIDResponse{Netid: &pb.SimpleInt{
		Value: uint64(m.NetworkID),
	}}, nil
}

// EpochNumLayers returns the number of layers per epoch (a network parameter)
func (m Mesh) EpochNumLayers(ctx context.Context, in *pb.EpochNumLayersRequest) (*pb.EpochNumLayersResponse, error) {
	m.log.Info("GRPC MeshService.EpochNumLayers")
	return &pb.EpochNumLayersResponse{
		Numlayers: &pb.SimpleInt{
			Value: uint64(m.LayersPerEpoch),
		},
	}, nil
}

// LayerDuration returns the layer duration in seconds (a network parameter)
func (m Mesh) LayerDuration(ctx context.Context, in *pb.LayerDurationRequest) (*pb.LayerDurationResponse, error) {
	m.log.Info("GRPC MeshService.LayerDuration")
	return &pb.LayerDurationResponse{
		Duration: &pb.SimpleInt{
			Value: uint64(m.LayerDurationSec),
		},
	}, nil
}

// MaxTransactionsPerSecond returns the max number of tx per sec (a network parameter)
func (m Mesh) MaxTransactionsPerSecond(ctx context.Context, in *pb.MaxTransactionsPerSecondRequest) (*pb.MaxTransactionsPerSecondResponse, error) {
	m.log.Info("GRPC MeshService.MaxTransactionsPerSecond")
	return &pb.MaxTransactionsPerSecondResponse{
		Maxtxpersecond: &pb.SimpleInt{
			Value: uint64(m.TxsPerBlock * m.LayerAvgSize / m.LayerDurationSec),
		},
	}, nil
}

// QUERIES

func (m Mesh) getFilteredTransactions(startLayer types.LayerID, addr types.Address) (txs []*types.Transaction, err error) {
	meshTxIds := m.getTxIdsFromMesh(startLayer, addr)
	mempoolTxIds := m.Mempool.GetTxIdsByAddress(addr)

	// Look up full data for all unique txids
	txs, missing := m.Mesh.GetTransactions(append(meshTxIds, mempoolTxIds...))

	// TODO: Do we ever expect txs to be missing here?
	// E.g., if this node has not synced/received them yet.
	if len(missing) != 0 {
		m.log.Error("could not find transactions %v", missing)
		return nil, status.Errorf(codes.Internal, "error retrieving tx data")
	}
	return
}

func (m Mesh) getFilteredActivations(startLayer types.LayerID, addr types.Address) (activations []*types.ActivationTx, err error) {
	// We have no way to look up activations by coinbase so we have no choice
	// but to read all of them.
	// TODO: index activations by layer (and maybe by coinbase)
	// See https://github.com/spacemeshos/go-spacemesh/issues/2064.
	var atxids []types.ATXID
	for l := startLayer; l <= m.Mesh.LatestLayer(); l++ {
		layer, err := m.Mesh.GetLayer(l)
		if layer == nil || err != nil {
			return nil, status.Errorf(codes.Internal, "error retrieving layer data")
		}

		for _, b := range layer.Blocks() {
			if b.ActiveSet != nil {
				atxids = append(atxids, *b.ActiveSet...)
			}
		}
	}

	// Look up full data
	atxs, matxs := m.Mesh.GetATXs(atxids)
	if len(matxs) != 0 {
		m.log.Error("could not find activations %v", matxs)
		return nil, status.Errorf(codes.Internal, "error retrieving activations data")
	}
	for _, atx := range atxs {
		// Filter here, now that we have full data
		if atx.Coinbase == addr {
			activations = append(activations, atx)
		}
	}
	return
}

// AccountMeshDataQuery returns account data
func (m Mesh) AccountMeshDataQuery(ctx context.Context, in *pb.AccountMeshDataQueryRequest) (*pb.AccountMeshDataQueryResponse, error) {
	m.log.Info("GRPC MeshService.AccountMeshDataQuery")

	startLayer := types.LayerID(in.MinLayer)

	if startLayer > m.Mesh.LatestLayer() {
		return nil, status.Errorf(codes.InvalidArgument, "`LatestLayer` must be less than or equal to latest layer")
	}
	if in.Filter == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Filter` must be provided")
	}
	if in.Filter.AccountId == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Filter.AccountId` must be provided")
	}
	if in.Filter.AccountMeshDataFlags == uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_UNSPECIFIED) {
		return nil, status.Errorf(codes.InvalidArgument, "`Filter.AccountMeshDataFlags` must set at least one bitfield")
	}

	// Read the filter flags
	filterTx := in.Filter.AccountMeshDataFlags&uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS) != 0
	filterActivations := in.Filter.AccountMeshDataFlags&uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS) != 0

	// Gather transaction data
	addr := types.BytesToAddress(in.Filter.AccountId.Address)
	res := &pb.AccountMeshDataQueryResponse{}
	if filterTx {
		txs, err := m.getFilteredTransactions(startLayer, addr)
		if err != nil {
			return nil, err
		}
		for _, t := range txs {
			res.Data = append(res.Data, &pb.AccountMeshData{
				Datum: &pb.AccountMeshData_Transaction{
					Transaction: transaction.Convert(t),
				},
			})
		}
	}

	// Gather activation data
	if filterActivations {
		atxs, err := m.getFilteredActivations(startLayer, addr)
		if err != nil {
			return nil, err
		}
		for _, atx := range atxs {
			pbatx, err := convertActivation(atx)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "error serializing activation data")
			}
			res.Data = append(res.Data, &pb.AccountMeshData{
				Datum: &pb.AccountMeshData_Activation{
					Activation: pbatx,
				},
			})
		}
	}

	// MAX RESULTS, OFFSET
	// There is some code duplication here as this is implemented in other Query endpoints,
	// but without generics, there's no clean way to do this for different types.

	res.TotalResults = uint32(len(res.Data))

	// Skip to offset, don't send more than max results
	// TODO: Optimize this. Obviously, we could do much smarter things than re-loading all
	// of the data from scratch, then figuring out which data to return here. We could cache
	// query results and/or figure out which data to load before loading it.
	offset := in.Offset

	// If the offset is too high there is nothing to return (this is not an error)
	if offset > uint32(len(res.Data)) {
		return &pb.AccountMeshDataQueryResponse{}, nil
	}

	// If the max results is too high, trim it. If MaxResults is zero, that means unlimited
	// (since we have no way to distinguish between zero and its not being provided).
	maxResults := in.MaxResults
	if maxResults == 0 || offset+maxResults > uint32(len(res.Data)) {
		maxResults = uint32(len(res.Data)) - offset
	}
	res.Data = res.Data[offset : offset+maxResults]
	return res, nil
}

func (s Mesh) getTxIdsFromMesh(minLayer types.LayerID, addr types.Address) []types.TransactionID {
	var txIDs []types.TransactionID
	for layerID := minLayer; layerID < s.Mesh.LatestLayer(); layerID++ {
		destTxIDs := s.Mesh.GetTransactionsByDestination(layerID, addr)
		txIDs = append(txIDs, destTxIDs...)
		originTxIds := s.Mesh.GetTransactionsByOrigin(layerID, addr)
		txIDs = append(txIDs, originTxIds...)
	}
	return txIDs
}

func convertActivation(a *types.ActivationTx) (*pb.Activation, error) {
	return &pb.Activation{
		Id:             &pb.ActivationId{Id: a.ID().Bytes()},
		Layer:          a.PubLayerID.Uint64(),
		SmesherId:      &pb.SmesherId{Id: a.NodeID.ToBytes()},
		Coinbase:       &pb.AccountId{Address: a.Coinbase.Bytes()},
		PrevAtx:        &pb.ActivationId{Id: a.PrevATXID.Bytes()},
		CommitmentSize: a.Nipst.Space,
	}, nil
}

func (m Mesh) readLayer(layer *types.Layer, layerStatus pb.Layer_LayerStatus) (*pb.Layer, error) {
	// Load all block data
	var blocks []*pb.Block

	// Save activations too
	var activations []types.ATXID

	for _, b := range layer.Blocks() {
		txs, missing := m.Mesh.GetTransactions(b.TxIDs)
		// TODO: Do we ever expect txs to be missing here?
		// E.g., if this node has not synced/received them yet.
		if len(missing) != 0 {
			m.log.Error("could not find transactions %v from layer %v", missing, layer.Index())
			return nil, status.Errorf(codes.Internal, "error retrieving tx data")
		}

		if b.ActiveSet != nil {
			activations = append(activations, *b.ActiveSet...)
		}

		var pbTxs []*pb.Transaction
		for _, t := range txs {
			pbTxs = append(pbTxs, transaction.Convert(t))
		}
		blocks = append(blocks, &pb.Block{
			Id:           b.ID().Bytes(),
			Transactions: pbTxs,
		})
	}

	// Extract ATX data from block data
	var pbActivations []*pb.Activation

	// Add unique ATXIDs
	atxs, matxs := m.Mesh.GetATXs(activations)
	if len(matxs) != 0 {
		m.log.Error("could not find activations %v from layer %v", matxs, layer.Index())
		return nil, status.Errorf(codes.Internal, "error retrieving activations data")
	}
	for _, atx := range atxs {
		pbatx, err := convertActivation(atx)
		if err != nil {
			m.log.Error("error serializing activation data: %s", err)
			return nil, status.Errorf(codes.Internal, "error serializing activation data")
		}
		pbActivations = append(pbActivations, pbatx)
	}

	stateRoot, err := m.Mesh.GetLayerStateRoot(layer.Index())
	if err != nil {
		// This is expected. We can only retrieve state root for a layer that was applied to state,
		// which only happens after it's approved/confirmed.
		m.log.With().Debug("no state root for layer",
			layer, log.String("status", layerStatus.String()), log.Err(err))
	}
	return &pb.Layer{
		Number:        layer.Index().Uint64(),
		Status:        layerStatus,
		Hash:          layer.Hash().Bytes(),
		Blocks:        blocks,
		Activations:   pbActivations,
		RootStateHash: stateRoot.Bytes(),
	}, nil
}

// LayersQuery returns all mesh data, layer by layer
func (m Mesh) LayersQuery(ctx context.Context, in *pb.LayersQueryRequest) (*pb.LayersQueryResponse, error) {
	m.log.Info("GRPC Mesh.LayersQuery")

	// Get the latest layers that passed both consensus engines.
	lastLayerPassedHare := m.Mesh.LatestLayerInState()
	lastLayerPassedTortoise := m.Mesh.ProcessedLayer()

	layers := []*pb.Layer{}
	for l := uint64(in.StartLayer); l <= uint64(in.EndLayer); l++ {
		layerStatus := pb.Layer_LAYER_STATUS_UNSPECIFIED

		// First check if the layer passed the Hare, then check if it passed the Tortoise.
		// It may be either, or both, but Tortoise always takes precedence.
		if l <= lastLayerPassedHare.Uint64() {
			layerStatus = pb.Layer_LAYER_STATUS_APPROVED
		}
		if l <= lastLayerPassedTortoise.Uint64() {
			layerStatus = pb.Layer_LAYER_STATUS_CONFIRMED
		}

		layer, err := m.Mesh.GetLayer(types.LayerID(l))
		// TODO: Be careful with how we handle missing layers here.
		// A layer that's newer than the currentLayer (defined above)
		// is clearly an input error. A missing layer that's older than
		// lastValidLayer is clearly an internal error. A missing layer
		// between these two is a gray area: do we define this as an
		// internal or an input error? For now, all missing layers produce
		// internal errors.
		if layer == nil || err != nil {
			m.log.Error("error retrieving layer data: %s", err)
			return nil, status.Errorf(codes.Internal, "error retrieving layer data")
		}

		pbLayer, err := m.readLayer(layer, layerStatus)
		if err != nil {
			return nil, err
		}
		layers = append(layers, pbLayer)
	}
	return &pb.LayersQueryResponse{Layer: layers}, nil
}

// STREAMS

// AccountMeshDataStream exposes a stream of transactions and activations for an account
func (m Mesh) AccountMeshDataStream(in *pb.AccountMeshDataStreamRequest, stream pb.MeshService_AccountMeshDataStreamServer) error {
	m.log.Info("GRPC MeshService.AccountMeshDataStream")

	if in.Filter == nil {
		return status.Errorf(codes.InvalidArgument, "`Filter` must be provided")
	}
	if in.Filter.AccountId == nil {
		return status.Errorf(codes.InvalidArgument, "`Filter.AccountId` must be provided")
	}
	if in.Filter.AccountMeshDataFlags == uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_UNSPECIFIED) {
		return status.Errorf(codes.InvalidArgument, "`Filter.AccountMeshDataFlags` must set at least one bitfield")
	}
	addr := types.BytesToAddress(in.Filter.AccountId.Address)

	// Read the filter flags
	filterTx := in.Filter.AccountMeshDataFlags&uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS) != 0
	filterActivations := in.Filter.AccountMeshDataFlags&uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS) != 0

	// Subscribe to the stream of transactions and activations
	var (
		txStream          chan events.TransactionWithValidity
		activationsStream chan *types.ActivationTx
	)
	if filterTx {
		txStream = events.GetNewTxChannel()
	}
	if filterActivations {
		activationsStream = events.GetActivationsChannel()
	}

	for {
		select {
		case activation, ok := <-activationsStream:
			if !ok {
				// we could handle this more gracefully, by no longer listening
				// to this stream but continuing to listen to the other stream,
				// but in practice one should never be closed while the other is
				// still running, so it doesn't matter
				m.log.Info("ActivationStream closed, shutting down")
				return nil
			}
			// Apply address filter
			if activation.Coinbase == addr {
				pbActivation, err := convertActivation(activation)
				if err != nil {
					errmsg := "error serializing activation data"
					m.log.Error(fmt.Sprintf("%s: %s", errmsg, err))
					return status.Errorf(codes.Internal, errmsg)
				}
				if err := stream.Send(&pb.AccountMeshDataStreamResponse{
					Datum: &pb.AccountMeshData{
						Datum: &pb.AccountMeshData_Activation{
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
				m.log.Info("NewTxStream closed, shutting down")
				return nil
			}
			// Apply address filter
			if tx.Valid && (tx.Transaction.Origin() == addr || tx.Transaction.Recipient == addr) {
				if err := stream.Send(&pb.AccountMeshDataStreamResponse{
					Datum: &pb.AccountMeshData{
						Datum: &pb.AccountMeshData_Transaction{
							Transaction: transaction.Convert(tx.Transaction),
						},
					},
				}); err != nil {
					return err
				}
			}
		case <-stream.Context().Done():
			m.log.Info("AccountMeshDataStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}

// LayerStream exposes a stream of all mesh data per layer
func (m Mesh) LayerStream(in *pb.LayerStreamRequest, stream pb.MeshService_LayerStreamServer) error {
	m.log.Info("GRPC MeshService.LayerStream")
	layerStream := events.GetLayerChannel()

	for {
		select {
		case layer, ok := <-layerStream:
			if !ok {
				m.log.Info("LayerStream closed, shutting down")
				return nil
			}
			pbLayer, err := m.readLayer(layer.Layer, convertLayerStatus(layer.Status))
			if err != nil {
				return err
			}
			if err := stream.Send(&pb.LayerStreamResponse{Layer: pbLayer}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			m.log.Info("LayerStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}

func convertLayerStatus(in int) pb.Layer_LayerStatus {
	switch in {
	case events.LayerStatusTypeApproved:
		return pb.Layer_LAYER_STATUS_APPROVED
	case events.LayerStatusTypeConfirmed:
		return pb.Layer_LAYER_STATUS_CONFIRMED
	default:
		return pb.Layer_LAYER_STATUS_UNSPECIFIED
	}
}
