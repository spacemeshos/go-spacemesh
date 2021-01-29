package grpcserver

import (
	"fmt"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MeshService exposes mesh data such as accounts, blocks, and transactions
type MeshService struct {
	Mesh             api.TxAPI // Mesh
	Mempool          api.MempoolAPI
	GenTime          api.GenesisTimeAPI
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

// NewMeshService creates a new service using config data
func NewMeshService(
	tx api.TxAPI, mempool api.MempoolAPI, genTime api.GenesisTimeAPI,
	layersPerEpoch int, networkID int8, layerDurationSec int,
	layerAvgSize int, txsPerBlock int) *MeshService {
	return &MeshService{
		Mesh:             tx,
		Mempool:          mempool,
		GenTime:          genTime,
		LayersPerEpoch:   layersPerEpoch,
		NetworkID:        networkID,
		LayerDurationSec: layerDurationSec,
		LayerAvgSize:     layerAvgSize,
		TxsPerBlock:      txsPerBlock,
	}
}

// GenesisTime returns the network genesis time as UNIX time
func (s MeshService) GenesisTime(context.Context, *pb.GenesisTimeRequest) (*pb.GenesisTimeResponse, error) {
	log.Info("GRPC MeshService.GenesisTime")
	return &pb.GenesisTimeResponse{Unixtime: &pb.SimpleInt{
		Value: uint64(s.GenTime.GetGenesisTime().Unix()),
	}}, nil
}

// CurrentLayer returns the current layer number
func (s MeshService) CurrentLayer(context.Context, *pb.CurrentLayerRequest) (*pb.CurrentLayerResponse, error) {
	log.Info("GRPC MeshService.CurrentLayer")
	return &pb.CurrentLayerResponse{Layernum: &pb.LayerNumber{
		Number: uint32(s.GenTime.GetCurrentLayer()),
	}}, nil
}

// CurrentEpoch returns the current epoch number
func (s MeshService) CurrentEpoch(context.Context, *pb.CurrentEpochRequest) (*pb.CurrentEpochResponse, error) {
	log.Info("GRPC MeshService.CurrentEpoch")
	curLayer := s.GenTime.GetCurrentLayer()
	return &pb.CurrentEpochResponse{Epochnum: &pb.SimpleInt{
		Value: uint64(curLayer.GetEpoch()),
	}}, nil
}

// NetID returns the network ID
func (s MeshService) NetID(context.Context, *pb.NetIDRequest) (*pb.NetIDResponse, error) {
	log.Info("GRPC MeshService.NetId")
	return &pb.NetIDResponse{Netid: &pb.SimpleInt{
		Value: uint64(s.NetworkID),
	}}, nil
}

// EpochNumLayers returns the number of layers per epoch (a network parameter)
func (s MeshService) EpochNumLayers(context.Context, *pb.EpochNumLayersRequest) (*pb.EpochNumLayersResponse, error) {
	log.Info("GRPC MeshService.EpochNumLayers")
	return &pb.EpochNumLayersResponse{Numlayers: &pb.SimpleInt{
		Value: uint64(s.LayersPerEpoch),
	}}, nil
}

// LayerDuration returns the layer duration in seconds (a network parameter)
func (s MeshService) LayerDuration(context.Context, *pb.LayerDurationRequest) (*pb.LayerDurationResponse, error) {
	log.Info("GRPC MeshService.LayerDuration")
	return &pb.LayerDurationResponse{Duration: &pb.SimpleInt{
		Value: uint64(s.LayerDurationSec),
	}}, nil
}

// MaxTransactionsPerSecond returns the max number of tx per sec (a network parameter)
func (s MeshService) MaxTransactionsPerSecond(context.Context, *pb.MaxTransactionsPerSecondRequest) (*pb.MaxTransactionsPerSecondResponse, error) {
	log.Info("GRPC MeshService.MaxTransactionsPerSecond")
	return &pb.MaxTransactionsPerSecondResponse{MaxTxsPerSecond: &pb.SimpleInt{
		Value: uint64(s.TxsPerBlock * s.LayerAvgSize / s.LayerDurationSec),
	}}, nil
}

// QUERIES

func (s MeshService) getFilteredTransactions(startLayer types.LayerID, addr types.Address) (txs []types.Transaction, err error) {
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
	return
}

func (s MeshService) getFilteredActivations(startLayer types.LayerID, addr types.Address) (activations []*types.ActivationTx, err error) {
	// We have no way to look up activations by coinbase so we have no choice
	// but to read all of them.
	// TODO: index activations by layer (and maybe by coinbase)
	// See https://github.com/spacemeshos/go-spacemesh/issues/2064.
	var atxids []types.ATXID
	for l := startLayer; l <= s.Mesh.LatestLayer(); l++ {
		layer, err := s.Mesh.GetLayer(l)
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
	atxs, matxs := s.Mesh.GetATXs(atxids)
	if len(matxs) != 0 {
		log.Error("could not find activations %v", matxs)
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
func (s MeshService) AccountMeshDataQuery(_ context.Context, in *pb.AccountMeshDataQueryRequest) (*pb.AccountMeshDataQueryResponse, error) {
	log.Info("GRPC MeshService.AccountMeshDataQuery")

	var startLayer types.LayerID
	if in.MinLayer != nil {
		startLayer = types.LayerID(in.MinLayer.Number)
	}

	if startLayer > s.Mesh.LatestLayer() {
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
		txs, err := s.getFilteredTransactions(startLayer, addr)
		if err != nil {
			return nil, err
		}
		for _, t := range txs {
			res.Data = append(res.Data, &pb.AccountMeshData{
				Datum: &pb.AccountMeshData_Transaction{
					Transaction: convertTransaction(t),
				},
			})
		}
	}

	// Gather activation data
	if filterActivations {
		atxs, err := s.getFilteredActivations(startLayer, addr)
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

func signingScheme(t types.Transaction) (scheme pb.Signature_Scheme) {
	scheme = pb.Signature_SCHEME_UNSPECIFIED
	switch t.Type().Signing {
	case types.EdSigningScheme:
		scheme = pb.Signature_SCHEME_ED25519
	case types.EdPlusSigningScheme:
		scheme = pb.Signature_SCHEME_ED25519_PLUS_PLUS
	}
	return
}

func convertTransaction(t types.Transaction) *pb.Transaction {
	return &pb.Transaction{
		Id: &pb.TransactionId{Id: t.ID().Bytes()},
		Datum: &pb.Transaction_CoinTransfer{
			CoinTransfer: &pb.CoinTransferTransaction{
				Receiver: &pb.AccountId{Address: t.GetRecipient().Bytes()},
			},
		},
		Sender: &pb.AccountId{Address: t.Origin().Bytes()},
		GasOffered: &pb.GasOffered{
			// We don't currently implement gas price. t.Fee is the gas actually paid
			// by the tx; GasLimit is the max gas. MeshService is concerned with the
			// pre-STF tx, which includes a gas offer but not an amount of gas actually
			// consumed.
			GasPrice:    t.GetGasPrice(),
			GasProvided: t.GetGasLimit(),
		},
		Amount:  &pb.Amount{Value: t.GetAmount()},
		Counter: t.GetNonce(), // TODO: nonce processing
		Signature: &pb.Signature{
			Scheme:    signingScheme(t),
			Signature: t.Signature().Bytes(),
			PublicKey: t.PubKey().Bytes(),
		},
	}
}

func convertActivation(a *types.ActivationTx) (*pb.Activation, error) {
	return &pb.Activation{
		Id:             &pb.ActivationId{Id: a.ID().Bytes()},
		Layer:          &pb.LayerNumber{Number: uint32(a.PubLayerID)},
		SmesherId:      &pb.SmesherId{Id: a.NodeID.ToBytes()},
		Coinbase:       &pb.AccountId{Address: a.Coinbase.Bytes()},
		PrevAtx:        &pb.ActivationId{Id: a.PrevATXID.Bytes()},
		CommitmentSize: a.Nipst.Space,
	}, nil
}

func (s MeshService) readLayer(layer *types.Layer, layerStatus pb.Layer_LayerStatus) (*pb.Layer, error) {
	// Load all block data
	var blocks []*pb.Block

	// Save activations too
	var activations []types.ATXID

	for _, b := range layer.Blocks() {
		txs, missing := s.Mesh.GetTransactions(b.TxIDs)
		// TODO: Do we ever expect txs to be missing here?
		// E.g., if this node has not synced/received them yet.
		if len(missing) != 0 {
			log.Error("could not find transactions %v from layer %v", missing, layer.Index())
			return nil, status.Errorf(codes.Internal, "error retrieving tx data")
		}

		if b.ActiveSet != nil {
			activations = append(activations, *b.ActiveSet...)
		}

		var pbTxs []*pb.Transaction
		for _, t := range txs {
			pbTxs = append(pbTxs, convertTransaction(t))
		}
		blocks = append(blocks, &pb.Block{
			Id:           types.Hash20(b.ID()).Bytes(),
			Transactions: pbTxs,
			SmesherId:    &pb.SmesherId{Id: b.MinerID().Bytes()},
			ActivationId: &pb.ActivationId{Id: b.ATXID.Bytes()},
		})
	}

	// Extract ATX data from block data
	var pbActivations []*pb.Activation

	// Add unique ATXIDs
	atxs, matxs := s.Mesh.GetATXs(activations)
	if len(matxs) != 0 {
		log.Error("could not find activations %v from layer %v", matxs, layer.Index())
		return nil, status.Errorf(codes.Internal, "error retrieving activations data")
	}
	for _, atx := range atxs {
		pbatx, err := convertActivation(atx)
		if err != nil {
			log.Error("error serializing activation data: %s", err)
			return nil, status.Errorf(codes.Internal, "error serializing activation data")
		}
		pbActivations = append(pbActivations, pbatx)
	}

	stateRoot, err := s.Mesh.GetLayerStateRoot(layer.Index())
	if err != nil {
		// This is expected. We can only retrieve state root for a layer that was applied to state,
		// which only happens after it's approved/confirmed.
		log.With().Debug("no state root for layer",
			layer, log.String("status", layerStatus.String()), log.Err(err))
	}
	return &pb.Layer{
		Number:        &pb.LayerNumber{Number: uint32(layer.Index())},
		Status:        layerStatus,
		Hash:          layer.Hash().Bytes(),
		Blocks:        blocks,
		Activations:   pbActivations,
		RootStateHash: stateRoot.Bytes(),
	}, nil
}

// LayersQuery returns all mesh data, layer by layer
func (s MeshService) LayersQuery(_ context.Context, in *pb.LayersQueryRequest) (*pb.LayersQueryResponse, error) {
	log.Info("GRPC MeshService.LayersQuery")

	var startLayer, endLayer types.LayerID
	if in.StartLayer != nil {
		startLayer = types.LayerID(in.StartLayer.Number)
	}
	if in.EndLayer != nil {
		endLayer = types.LayerID(in.EndLayer.Number)
	}

	// Get the latest layers that passed both consensus engines.
	lastLayerPassedHare := s.Mesh.LatestLayerInState()
	lastLayerPassedTortoise := s.Mesh.ProcessedLayer()

	layers := []*pb.Layer{}
	for l := uint64(startLayer); l <= uint64(endLayer); l++ {
		layerStatus := pb.Layer_LAYER_STATUS_UNSPECIFIED

		// First check if the layer passed the Hare, then check if it passed the Tortoise.
		// It may be either, or both, but Tortoise always takes precedence.
		if l <= lastLayerPassedHare.Uint64() {
			layerStatus = pb.Layer_LAYER_STATUS_APPROVED
		}
		if l <= lastLayerPassedTortoise.Uint64() {
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
			log.Error("error retrieving layer data: %s", err)
			return nil, status.Errorf(codes.Internal, "error retrieving layer data")
		}

		pbLayer, err := s.readLayer(layer, layerStatus)
		if err != nil {
			return nil, err
		}
		layers = append(layers, pbLayer)
	}
	return &pb.LayersQueryResponse{Layer: layers}, nil
}

// STREAMS

// AccountMeshDataStream exposes a stream of transactions and activations for an account
func (s MeshService) AccountMeshDataStream(in *pb.AccountMeshDataStreamRequest, stream pb.MeshService_AccountMeshDataStreamServer) error {
	log.Info("GRPC MeshService.AccountMeshDataStream")

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
				log.Info("ActivationStream closed, shutting down")
				return nil
			}
			// Apply address filter
			if activation.Coinbase == addr {
				pbActivation, err := convertActivation(activation)
				if err != nil {
					errmsg := "error serializing activation data"
					log.Error(fmt.Sprintf("%s: %s", errmsg, err))
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
				log.Info("NewTxStream closed, shutting down")
				return nil
			}
			// Apply address filter
			if tx.Valid && (tx.Transaction.Origin() == addr || tx.Transaction.GetRecipient() == addr) {
				if err := stream.Send(&pb.AccountMeshDataStreamResponse{
					Datum: &pb.AccountMeshData{
						Datum: &pb.AccountMeshData_Transaction{
							Transaction: convertTransaction(tx.Transaction),
						},
					},
				}); err != nil {
					return err
				}
			}
		case <-stream.Context().Done():
			log.Info("AccountMeshDataStream closing stream, client disconnected")
			return nil
		}
		// TODO: do we need an additional case here for a context to indicate
		// that the service needs to shut down?
	}
}

// LayerStream exposes a stream of all mesh data per layer
func (s MeshService) LayerStream(_ *pb.LayerStreamRequest, stream pb.MeshService_LayerStreamServer) error {
	log.Info("GRPC MeshService.LayerStream")
	layerStream := events.GetLayerChannel()

	for {
		select {
		case layer, ok := <-layerStream:
			if !ok {
				log.Info("LayerStream closed, shutting down")
				return nil
			}
			pbLayer, err := s.readLayer(layer.Layer, convertLayerStatus(layer.Status))
			if err != nil {
				return err
			}
			if err := stream.Send(&pb.LayerStreamResponse{Layer: pbLayer}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			log.Info("LayerStream closing stream, client disconnected")
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
