package grpcserver

import (
	"context"
	"fmt"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

// MeshService exposes mesh data such as accounts, blocks, and transactions.
type MeshService struct {
	mesh           meshAPI // Mesh
	conState       conservativeState
	genTime        genesisTimeAPI
	layersPerEpoch uint32
	genesisID      types.Hash20
	layerDuration  time.Duration
	layerAvgSize   uint32
	txsPerProposal uint32
}

// RegisterService registers this service with a grpc server instance.
func (s MeshService) RegisterService(server *Server) {
	pb.RegisterMeshServiceServer(server.GrpcServer, s)
}

// NewMeshService creates a new service using config data.
func NewMeshService(
	msh meshAPI, cstate conservativeState, genTime genesisTimeAPI,
	layersPerEpoch uint32, genesisID types.Hash20, layerDuration time.Duration,
	layerAvgSize, txsPerProposal uint32,
) *MeshService {
	return &MeshService{
		mesh:           msh,
		conState:       cstate,
		genTime:        genTime,
		layersPerEpoch: layersPerEpoch,
		genesisID:      genesisID,
		layerDuration:  layerDuration,
		layerAvgSize:   layerAvgSize,
		txsPerProposal: txsPerProposal,
	}
}

// GenesisTime returns the network genesis time as UNIX time.
func (s MeshService) GenesisTime(context.Context, *pb.GenesisTimeRequest) (*pb.GenesisTimeResponse, error) {
	log.Info("GRPC MeshService.GenesisTime")
	return &pb.GenesisTimeResponse{Unixtime: &pb.SimpleInt{
		Value: uint64(s.genTime.GenesisTime().Unix()),
	}}, nil
}

// CurrentLayer returns the current layer number.
func (s MeshService) CurrentLayer(context.Context, *pb.CurrentLayerRequest) (*pb.CurrentLayerResponse, error) {
	log.Info("GRPC MeshService.CurrentLayer")
	return &pb.CurrentLayerResponse{Layernum: &pb.LayerNumber{
		Number: uint32(s.genTime.CurrentLayer().Uint32()),
	}}, nil
}

// CurrentEpoch returns the current epoch number.
func (s MeshService) CurrentEpoch(context.Context, *pb.CurrentEpochRequest) (*pb.CurrentEpochResponse, error) {
	log.Info("GRPC MeshService.CurrentEpoch")
	curLayer := s.genTime.CurrentLayer()
	return &pb.CurrentEpochResponse{Epochnum: &pb.EpochNumber{
		Number: curLayer.GetEpoch().Uint32(),
	}}, nil
}

// GenesisID returns the network ID.
func (s MeshService) GenesisID(context.Context, *pb.GenesisIDRequest) (*pb.GenesisIDResponse, error) {
	log.Info("GRPC MeshService.NetId")
	return &pb.GenesisIDResponse{GenesisId: s.genesisID.Bytes()}, nil
}

// EpochNumLayers returns the number of layers per epoch (a network parameter).
func (s MeshService) EpochNumLayers(context.Context, *pb.EpochNumLayersRequest) (*pb.EpochNumLayersResponse, error) {
	log.Info("GRPC MeshService.EpochNumLayers")
	return &pb.EpochNumLayersResponse{Numlayers: &pb.LayerNumber{
		Number: s.layersPerEpoch,
	}}, nil
}

// LayerDuration returns the layer duration in seconds (a network parameter).
func (s MeshService) LayerDuration(context.Context, *pb.LayerDurationRequest) (*pb.LayerDurationResponse, error) {
	log.Info("GRPC MeshService.LayerDuration")
	return &pb.LayerDurationResponse{Duration: &pb.SimpleInt{
		Value: uint64(s.layerDuration.Seconds()),
	}}, nil
}

// MaxTransactionsPerSecond returns the max number of tx per sec (a network parameter).
func (s MeshService) MaxTransactionsPerSecond(context.Context, *pb.MaxTransactionsPerSecondRequest) (*pb.MaxTransactionsPerSecondResponse, error) {
	log.Info("GRPC MeshService.MaxTransactionsPerSecond")
	return &pb.MaxTransactionsPerSecondResponse{MaxTxsPerSecond: &pb.SimpleInt{
		Value: uint64(s.txsPerProposal * s.layerAvgSize / uint32(s.layerDuration.Seconds())),
	}}, nil
}

// QUERIES

func (s MeshService) getFilteredTransactions(from types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	latest := s.mesh.LatestLayer()
	txs, err := s.conState.GetTransactionsByAddress(from, latest, address)
	if err != nil {
		return nil, fmt.Errorf("reading txs for address %s: %w", address, err)
	}
	return txs, nil
}

func (s MeshService) getFilteredActivations(ctx context.Context, startLayer types.LayerID, addr types.Address) (activations []*types.VerifiedActivationTx, err error) {
	// We have no way to look up activations by coinbase so we have no choice but to read all of them.
	// TODO: index activations by layer (and maybe by coinbase)
	// See https://github.com/spacemeshos/go-spacemesh/issues/2064.
	var atxids []types.ATXID
	for l := startLayer; !l.After(s.mesh.LatestLayer()); l = l.Add(1) {
		layer, err := s.mesh.GetLayer(l)
		if layer == nil || err != nil {
			return nil, status.Errorf(codes.Internal, "error retrieving layer data")
		}
		for _, b := range layer.Ballots() {
			if b.EpochData != nil && b.ActiveSet != nil {
				atxids = append(atxids, b.ActiveSet...)
			}
		}
	}

	// Look up full data
	atxs, matxs := s.mesh.GetATXs(ctx, atxids)
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

// AccountMeshDataQuery returns account data.
func (s MeshService) AccountMeshDataQuery(ctx context.Context, in *pb.AccountMeshDataQueryRequest) (*pb.AccountMeshDataQueryResponse, error) {
	log.Info("GRPC MeshService.AccountMeshDataQuery")

	var startLayer types.LayerID
	if in.MinLayer != nil {
		startLayer = types.LayerID(in.MinLayer.Number)
	}

	if startLayer.After(s.mesh.LatestLayer()) {
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
	addr, err := types.StringToAddress(in.Filter.AccountId.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Filter.AccountId.Address `%s`: %w", in.Filter.AccountId.Address, err)
	}
	res := &pb.AccountMeshDataQueryResponse{}
	if filterTx {
		txs, err := s.getFilteredTransactions(startLayer, addr)
		if err != nil {
			return nil, err
		}
		for _, t := range txs {
			res.Data = append(res.Data, &pb.AccountMeshData{
				Datum: &pb.AccountMeshData_MeshTransaction{
					MeshTransaction: &pb.MeshTransaction{
						Transaction: castTransaction(&t.Transaction),
						LayerId:     &pb.LayerNumber{Number: t.LayerID.Uint32()},
					},
				},
			})
		}
	}

	// Gather activation data
	if filterActivations {
		atxs, err := s.getFilteredActivations(ctx, startLayer, addr)
		if err != nil {
			return nil, err
		}
		for _, atx := range atxs {
			res.Data = append(res.Data, &pb.AccountMeshData{
				Datum: &pb.AccountMeshData_Activation{
					Activation: convertActivation(atx),
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

func convertLayerID(l types.LayerID) *pb.LayerNumber {
	if layerID := l.Uint32(); layerID != 0 {
		return &pb.LayerNumber{Number: layerID}
	}

	return nil
}

func castTransaction(t *types.Transaction) *pb.Transaction {
	tx := &pb.Transaction{
		Id:  t.ID[:],
		Raw: t.Raw,
	}
	if t.TxHeader != nil {
		tx.Principal = &pb.AccountId{
			Address: t.Principal.String(),
		}
		tx.Template = &pb.AccountId{
			Address: t.TemplateAddress.String(),
		}
		tx.Method = uint32(t.Method)
		tx.Nonce = &pb.Nonce{
			Counter: t.Nonce,
		}
		tx.Limits = &pb.LayerLimits{
			Min: t.LayerLimits.Min,
			Max: t.LayerLimits.Max,
		}
		tx.MaxGas = t.MaxGas
		tx.GasPrice = t.GasPrice
		tx.MaxSpend = t.MaxSpend
	}
	return tx
}

func convertActivation(a *types.VerifiedActivationTx) *pb.Activation {
	return &pb.Activation{
		Id:        &pb.ActivationId{Id: a.ID().Bytes()},
		Layer:     &pb.LayerNumber{Number: a.PublishEpoch.Uint32()},
		SmesherId: &pb.SmesherId{Id: a.SmesherID.Bytes()},
		Coinbase:  &pb.AccountId{Address: a.Coinbase.String()},
		PrevAtx:   &pb.ActivationId{Id: a.PrevATXID.Bytes()},
		NumUnits:  uint32(a.NumUnits),
		Sequence:  a.Sequence,
	}
}

func (s MeshService) readLayer(ctx context.Context, layerID types.LayerID, layerStatus pb.Layer_LayerStatus) (*pb.Layer, error) {
	// Load all block data
	var blocks []*pb.Block

	// Save activations too
	var activations []types.ATXID

	// read layer blocks
	layer, err := s.mesh.GetLayer(layerID)
	// TODO: Be careful with how we handle missing layers here.
	// A layer that's newer than the currentLayer (defined above)
	// is clearly an input error. A missing layer that's older than
	// lastValidLayer is clearly an internal error. A missing layer
	// between these two is a gray area: do we define this as an
	// internal or an input error? For now, all missing layers produce
	// internal errors.
	if err != nil {
		log.With().Error("could not read layer from database", layerID, log.Err(err))
		return nil, status.Errorf(codes.Internal, "error reading layer data")
	}

	// TODO add proposal data as needed.

	for _, b := range layer.Blocks() {
		mtxs, missing := s.conState.GetMeshTransactions(b.TxIDs)
		// TODO: Do we ever expect txs to be missing here?
		// E.g., if this node has not synced/received them yet.
		if len(missing) != 0 {
			log.With().Error("could not find transactions from layer",
				log.String("missing", fmt.Sprint(missing)), layer.Index())
			return nil, status.Errorf(codes.Internal, "error retrieving tx data")
		}

		pbTxs := make([]*pb.Transaction, 0, len(mtxs))
		for _, t := range mtxs {
			pbTxs = append(pbTxs, castTransaction(&t.Transaction))
		}
		blocks = append(blocks, &pb.Block{
			Id:           types.Hash20(b.ID()).Bytes(),
			Transactions: pbTxs,
		})
	}

	for _, b := range layer.Ballots() {
		if b.EpochData != nil && b.ActiveSet != nil {
			activations = append(activations, b.ActiveSet...)
		}
	}

	// Extract ATX data from block data
	var pbActivations []*pb.Activation

	// Add unique ATXIDs
	atxs, matxs := s.mesh.GetATXs(ctx, activations)
	if len(matxs) != 0 {
		log.With().Error("could not find activations from layer",
			log.String("missing", fmt.Sprint(matxs)), layer.Index())
		return nil, status.Errorf(codes.Internal, "error retrieving activations data")
	}
	for _, atx := range atxs {
		pbActivations = append(pbActivations, convertActivation(atx))
	}

	stateRoot, err := s.conState.GetLayerStateRoot(layer.Index())
	if err != nil {
		// This is expected. We can only retrieve state root for a layer that was applied to state,
		// which only happens after it's approved/confirmed.
		log.With().Debug("no state root for layer",
			layer, log.String("status", layerStatus.String()), log.Err(err))
	}
	hash, err := s.mesh.MeshHash(layerID)
	if err != nil {
		// This is expected. We can only retrieve state root for a layer that was applied to state,
		// which only happens after it's approved/confirmed.
		log.With().Debug("no mesh hash at layer",
			layer, log.String("status", layerStatus.String()), log.Err(err))
	}
	return &pb.Layer{
		Number:        &pb.LayerNumber{Number: layer.Index().Uint32()},
		Status:        layerStatus,
		Blocks:        blocks,
		Activations:   pbActivations,
		Hash:          hash.Bytes(),
		RootStateHash: stateRoot.Bytes(),
	}, nil
}

// LayersQuery returns all mesh data, layer by layer.
func (s MeshService) LayersQuery(ctx context.Context, in *pb.LayersQueryRequest) (*pb.LayersQueryResponse, error) {
	log.Info("GRPC MeshService.LayersQuery")

	var startLayer, endLayer types.LayerID
	if in.StartLayer != nil {
		startLayer = types.LayerID(in.StartLayer.Number)
	}
	if in.EndLayer != nil {
		endLayer = types.LayerID(in.EndLayer.Number)
	}

	// Get the latest layers that passed both consensus engines.
	lastLayerPassedHare := s.mesh.LatestLayerInState()
	lastLayerPassedTortoise := s.mesh.ProcessedLayer()

	var layers []*pb.Layer
	for l := startLayer; !l.After(endLayer); l = l.Add(1) {
		layerStatus := pb.Layer_LAYER_STATUS_UNSPECIFIED

		// First check if the layer passed the Hare, then check if it passed the Tortoise.
		// It may be either, or both, but Tortoise always takes precedence.
		if !l.After(lastLayerPassedHare) {
			layerStatus = pb.Layer_LAYER_STATUS_APPROVED
		}
		if !l.After(lastLayerPassedTortoise) {
			layerStatus = pb.Layer_LAYER_STATUS_CONFIRMED
		}

		layer, err := s.mesh.GetLayer(l)
		// TODO: Be careful with how we handle missing layers here.
		// A layer that's newer than the currentLayer (defined above)
		// is clearly an input error. A missing layer that's older than
		// lastValidLayer is clearly an internal error. A missing layer
		// between these two is a gray area: do we define this as an
		// internal or an input error? For now, all missing layers produce
		// internal errors.
		if layer == nil || err != nil {
			log.With().Error("error retrieving layer data", log.Err(err))
			return nil, status.Errorf(codes.Internal, "error retrieving layer data")
		}

		pbLayer, err := s.readLayer(ctx, l, layerStatus)
		if err != nil {
			return nil, err
		}
		layers = append(layers, pbLayer)
	}
	return &pb.LayersQueryResponse{Layer: layers}, nil
}

// STREAMS

// AccountMeshDataStream exposes a stream of transactions and activations for an account.
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
	addr, err := types.StringToAddress(in.Filter.AccountId.Address)
	if err != nil {
		return fmt.Errorf("invalid in.Filter.AccountId.Address `%s`: %w", in.Filter.AccountId.Address, err)
	}

	// Read the filter flags
	filterTx := in.Filter.AccountMeshDataFlags&uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS) != 0
	filterActivations := in.Filter.AccountMeshDataFlags&uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS) != 0

	// Subscribe to the stream of transactions and activations
	var (
		txCh                          <-chan events.Transaction
		activationsCh                 <-chan events.ActivationTx
		txBufFull, activationsBufFull <-chan struct{}
	)

	if filterTx {
		if txsSubscription := events.SubscribeTxs(); txsSubscription != nil {
			txCh, txBufFull = consumeEvents[events.Transaction](stream.Context(), txsSubscription)
		}
	}
	if filterActivations {
		if activationsSubscription := events.SubscribeActivations(); activationsSubscription != nil {
			activationsCh, activationsBufFull = consumeEvents[events.ActivationTx](stream.Context(), activationsSubscription)
		}
	}

	for {
		select {
		case <-txBufFull:
			log.Info("tx buffer is full, shutting down")
			return status.Error(codes.Canceled, errTxBufferFull)
		case <-activationsBufFull:
			log.Info("activations buffer is full, shutting down")
			return status.Error(codes.Canceled, errActivationsBufferFull)
		case activationEvent := <-activationsCh:
			activation := activationEvent.VerifiedActivationTx
			// Apply address filter
			if activation.Coinbase == addr {
				resp := &pb.AccountMeshDataStreamResponse{
					Datum: &pb.AccountMeshData{
						Datum: &pb.AccountMeshData_Activation{
							Activation: convertActivation(activation),
						},
					},
				}
				if err := stream.Send(resp); err != nil {
					return fmt.Errorf("send to stream: %w", err)
				}
			}
		case tx := <-txCh:
			// Apply address filter
			if tx.Valid && tx.Transaction.TxHeader != nil && tx.Transaction.Principal == addr {
				resp := &pb.AccountMeshDataStreamResponse{
					Datum: &pb.AccountMeshData{
						Datum: &pb.AccountMeshData_MeshTransaction{
							MeshTransaction: &pb.MeshTransaction{
								Transaction: castTransaction(tx.Transaction),
								LayerId:     convertLayerID(tx.LayerID),
							},
						},
					},
				}
				if err := stream.Send(resp); err != nil {
					return fmt.Errorf("send to stream: %w", err)
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

// LayerStream exposes a stream of all mesh data per layer.
func (s MeshService) LayerStream(_ *pb.LayerStreamRequest, stream pb.MeshService_LayerStreamServer) error {
	log.Info("GRPC MeshService.LayerStream")

	var (
		layerCh       <-chan events.LayerUpdate
		layersBufFull <-chan struct{}
	)

	if layersSubscription := events.SubscribeLayers(); layersSubscription != nil {
		layerCh, layersBufFull = consumeEvents[events.LayerUpdate](stream.Context(), layersSubscription)
	}

	for {
		select {
		case <-layersBufFull:
			log.Info("layer buffer is full, shutting down")
			return status.Error(codes.Canceled, errAccountBufferFull)
		case layer, ok := <-layerCh:
			if !ok {
				log.Info("LayerStream closed, shutting down")
				return nil
			}
			pbLayer, err := s.readLayer(stream.Context(), layer.LayerID, convertLayerStatus(layer.Status))
			if err != nil {
				return fmt.Errorf("read layer: %w", err)
			}

			if err := stream.Send(&pb.LayerStreamResponse{Layer: pbLayer}); err != nil {
				return fmt.Errorf("send to stream: %w", err)
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
	case events.LayerStatusTypeApplied:
		return pb.Layer_LAYER_STATUS_APPLIED
	default:
		return pb.Layer_LAYER_STATUS_UNSPECIFIED
	}
}

func (s MeshService) EpochStream(req *pb.EpochStreamRequest, stream pb.MeshService_EpochStreamServer) error {
	epoch := types.EpochID(req.Epoch)
	atxids, err := s.mesh.EpochAtxs(epoch)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	for _, id := range atxids {
		select {
		case <-stream.Context().Done():
			return nil
		default:
			var res pb.EpochStreamResponse
			res.Id = &pb.ActivationId{Id: id.Bytes()}
			if err = stream.Send(&res); err != nil {
				return status.Error(codes.Internal, err.Error())
			}
		}
	}
	return nil
}
