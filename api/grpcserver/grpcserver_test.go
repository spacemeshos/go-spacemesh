package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	ma "github.com/multiformats/go-multiaddr"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/sync/errgroup"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/txs"
)

const (
	labelsPerUnit  = 2048
	numUnits       = 2
	genTimeUnix    = 1000000
	layerDuration  = 10 * time.Second
	layerAvgSize   = 10
	txsPerProposal = 99
	layersPerEpoch = uint32(5)

	// for now LayersStream returns no ATXs.
	atxPerLayer = 0

	// LayersStream returns one effective block per layer.
	blkPerLayer    = 1
	accountBalance = 8675301
	accountCounter = 0
	rewardAmount   = 5551234
)

var (
	txReturnLayer    = types.LayerID(1)
	layerFirst       = types.LayerID(0)
	layerVerified    = types.LayerID(8)
	layerLatest      = types.LayerID(10)
	layerCurrent     = types.LayerID(12)
	postGenesisEpoch = types.EpochID(2)
	genesisID        = types.Hash20{}

	addr1           types.Address
	addr2           types.Address
	rewardSmesherID = types.RandomNodeID()
	prevAtxID       = types.ATXID(types.HexToHash32("44444"))
	chlng           = types.HexToHash32("55555")
	poetRef         = []byte("66666")
	nipost          = newNIPostWithChallenge(&chlng, poetRef)
	challenge       = newChallenge(1, prevAtxID, prevAtxID, postGenesisEpoch)
	globalAtx       *types.VerifiedActivationTx
	globalAtx2      *types.VerifiedActivationTx
	globalTx        *types.Transaction
	globalTx2       *types.Transaction
	ballot1         = genLayerBallot(types.LayerID(11))
	block1          = genLayerBlock(types.LayerID(11), nil)
	block2          = genLayerBlock(types.LayerID(11), nil)
	block3          = genLayerBlock(types.LayerID(11), nil)
	meshAPIMock     = &MeshAPIMock{}
	conStateAPI     = &ConStateAPIMock{
		returnTx:      make(map[types.TransactionID]*types.Transaction),
		layerApplied:  make(map[types.TransactionID]*types.LayerID),
		balances:      make(map[types.Address]*big.Int),
		nonces:        make(map[types.Address]uint64),
		poolByAddress: make(map[types.Address]types.TransactionID),
		poolByTxId:    make(map[types.TransactionID]*types.Transaction),
	}
	stateRoot = types.HexToHash32("11111")
)

func genLayerBallot(layerID types.LayerID) *types.Ballot {
	b := types.RandomBallot()
	b.Layer = layerID
	signer, _ := signing.NewEdSigner()
	b.Signature = signer.Sign(signing.BALLOT, b.SignedBytes())
	b.SmesherID = signer.NodeID()
	b.Initialize()
	return b
}

func genLayerBlock(layerID types.LayerID, txs []types.TransactionID) *types.Block {
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			TxIDs:      txs,
		},
	}
	b.Initialize()
	return b
}

func dialGrpc(ctx context.Context, tb testing.TB, cfg Config) *grpc.ClientConn {
	tb.Helper()
	conn, err := grpc.DialContext(ctx,
		cfg.PublicListener,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(tb, err)
	tb.Cleanup(func() { require.NoError(tb, conn.Close()) })
	return conn
}

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	var err error
	signer, err := signing.NewEdSigner()
	if err != nil {
		log.Println("failed to create signer:", err)
		os.Exit(1)
	}
	signer1, err := signing.NewEdSigner()
	if err != nil {
		log.Println("failed to create signer:", err)
		os.Exit(1)
	}
	signer2, err := signing.NewEdSigner()
	if err != nil {
		log.Println("failed to create signer:", err)
		os.Exit(1)
	}

	addr1 = wallet.Address(signer1.PublicKey().Bytes())
	addr2 = wallet.Address(signer2.PublicKey().Bytes())

	atx := types.NewActivationTx(challenge, addr1, nipost, numUnits, nil)
	atx.SetEffectiveNumUnits(numUnits)
	atx.SetReceived(time.Now())
	if err := activation.SignAndFinalizeAtx(signer, atx); err != nil {
		log.Println("failed to sign atx:", err)
		os.Exit(1)
	}
	globalAtx, err = atx.Verify(0, 1)
	if err != nil {
		log.Println("failed to verify atx:", err)
		os.Exit(1)
	}

	atx2 := types.NewActivationTx(challenge, addr2, nipost, numUnits, nil)
	atx2.SetEffectiveNumUnits(numUnits)
	atx2.SetReceived(time.Now())
	if err := activation.SignAndFinalizeAtx(signer, atx2); err != nil {
		log.Println("failed to sign atx:", err)
		os.Exit(1)
	}
	globalAtx2, err = atx2.Verify(0, 1)
	if err != nil {
		log.Println("failed to verify atx:", err)
		os.Exit(1)
	}

	// These create circular dependencies so they have to be initialized
	// after the global vars
	ballot1.AtxID = globalAtx.ID()
	ballot1.EpochData = &types.EpochData{ActiveSetHash: types.ATXIDList{globalAtx.ID(), globalAtx2.ID()}.Hash()}

	globalTx = NewTx(0, addr1, signer1)
	globalTx2 = NewTx(1, addr2, signer2)

	block1.TxIDs = []types.TransactionID{globalTx.ID, globalTx2.ID}
	conStateAPI.returnTx[globalTx.ID] = globalTx
	conStateAPI.returnTx[globalTx2.ID] = globalTx2
	conStateAPI.balances[addr1] = big.NewInt(int64(accountBalance))
	conStateAPI.balances[addr2] = big.NewInt(int64(accountBalance))
	conStateAPI.nonces[globalTx.Principal] = uint64(accountCounter)

	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

func newNIPostWithChallenge(challenge *types.Hash32, poetRef []byte) *types.NIPost {
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(shared.HashMembershipTreeNode).
		WithLeavesToProve(map[uint64]bool{0: true}).
		Build()
	if err != nil {
		panic("failed to add leaf to tree")
	}
	if err := tree.AddLeaf(challenge[:]); err != nil {
		panic("failed to add leaf to tree")
	}
	nodes := tree.Proof()
	nodesH32 := make([]types.Hash32, 0, len(nodes))
	for _, n := range nodes {
		nodesH32 = append(nodesH32, types.BytesToHash(n))
	}
	return &types.NIPost{
		Membership: types.MerkleProof{
			Nodes: nodesH32,
		},
		Post: &types.Post{
			Nonce:   0,
			Indices: []byte(nil),
		},
		PostMetadata: &types.PostMetadata{
			Challenge:     poetRef,
			LabelsPerUnit: labelsPerUnit,
		},
	}
}

type MeshAPIMock struct{}

// latest layer received.
func (m *MeshAPIMock) LatestLayer() types.LayerID {
	return layerLatest
}

// latest layer approved/confirmed/applied to state
// The real logic here is a bit more complicated, as it depends whether the node
// is syncing or not. If it's not syncing, layers are applied to state as they're
// verified by Hare. If it's syncing, Hare is not run, and they are applied to
// state as they're confirmed by Tortoise and it advances pbase. This is all in
// flux right now so keep this simple for the purposes of testing.
func (m *MeshAPIMock) LatestLayerInState() types.LayerID {
	return layerVerified
}

func (m *MeshAPIMock) ProcessedLayer() types.LayerID {
	return layerVerified
}

func (m *MeshAPIMock) GetRewardsByCoinbase(types.Address) (rewards []*types.Reward, err error) {
	return []*types.Reward{
		{
			Layer:       layerFirst,
			TotalReward: rewardAmount,
			LayerReward: rewardAmount,
			Coinbase:    addr1,
			SmesherID:   rewardSmesherID,
		},
	}, nil
}

func (m *MeshAPIMock) GetRewardsBySmesherId(types.NodeID) (rewards []*types.Reward, err error) {
	return []*types.Reward{
		{
			Layer:       layerFirst,
			TotalReward: rewardAmount,
			LayerReward: rewardAmount,
			Coinbase:    addr1,
			SmesherID:   rewardSmesherID,
		},
	}, nil
}

func (m *MeshAPIMock) GetLayer(tid types.LayerID) (*types.Layer, error) {
	if tid.After(layerCurrent) {
		return nil, errors.New("requested layer later than current layer")
	} else if tid.After(m.LatestLayer()) {
		return nil, errors.New("haven't received that layer yet")
	}

	ballots := []*types.Ballot{ballot1}
	blocks := []*types.Block{block1, block2, block3}
	return types.NewExistingLayer(tid, ballots, blocks), nil
}

func (m *MeshAPIMock) GetLayerVerified(tid types.LayerID) (*types.Block, error) {
	return block1, nil
}

func (m *MeshAPIMock) GetATXs(
	context.Context,
	[]types.ATXID,
) (map[types.ATXID]*types.VerifiedActivationTx, []types.ATXID) {
	atxs := map[types.ATXID]*types.VerifiedActivationTx{
		globalAtx.ID():  globalAtx,
		globalAtx2.ID(): globalAtx2,
	}
	return atxs, nil
}

func (m *MeshAPIMock) MeshHash(types.LayerID) (types.Hash32, error) {
	return types.RandomHash(), nil
}

type ConStateAPIMock struct {
	returnTx     map[types.TransactionID]*types.Transaction
	layerApplied map[types.TransactionID]*types.LayerID
	balances     map[types.Address]*big.Int
	nonces       map[types.Address]uint64

	// In the real txs.txPool struct, there are multiple data structures and they're more complex,
	// but we just mock a very simple use case here and only store some of these data
	poolByAddress map[types.Address]types.TransactionID
	poolByTxId    map[types.TransactionID]*types.Transaction
}

func (t *ConStateAPIMock) put(id types.TransactionID, tx *types.Transaction) {
	t.poolByTxId[id] = tx
	t.poolByAddress[tx.Principal] = id
	events.ReportNewTx(0, tx)
}

// Return a mock estimated nonce and balance that's different than the default, mimicking transactions that are
// unconfirmed or in the mempool that will update state.
func (t *ConStateAPIMock) GetProjection(types.Address) (uint64, uint64) {
	return accountCounter + 1, accountBalance + 1
}

func (t *ConStateAPIMock) GetAllAccounts() (res []*types.Account, err error) {
	for address, balance := range t.balances {
		res = append(res, &types.Account{
			Address:   address,
			Balance:   balance.Uint64(),
			NextNonce: t.nonces[address],
		})
	}
	return res, nil
}

func (t *ConStateAPIMock) GetStateRoot() (types.Hash32, error) {
	return stateRoot, nil
}

func (t *ConStateAPIMock) GetMeshTransaction(id types.TransactionID) (*types.MeshTransaction, error) {
	tx, ok := t.returnTx[id]
	if ok {
		return &types.MeshTransaction{Transaction: *tx, State: types.APPLIED}, nil
	}
	tx, ok = t.poolByTxId[id]
	if ok {
		return &types.MeshTransaction{Transaction: *tx, State: types.MEMPOOL}, nil
	}
	return nil, errors.New("it ain't there")
}

func (t *ConStateAPIMock) GetTransactionsByAddress(
	from, to types.LayerID,
	account types.Address,
) ([]*types.MeshTransaction, error) {
	if from.After(txReturnLayer) {
		return nil, nil
	}
	var txs []*types.MeshTransaction
	for _, tx := range t.returnTx {
		if tx.Principal.String() == account.String() {
			txs = append(txs, &types.MeshTransaction{Transaction: *tx})
		}
	}
	return txs, nil
}

func (t *ConStateAPIMock) GetMeshTransactions(
	txIds []types.TransactionID,
) (txs []*types.MeshTransaction, missing map[types.TransactionID]struct{}) {
	for _, txId := range txIds {
		for _, tx := range t.returnTx {
			if tx.ID == txId {
				txs = append(txs, &types.MeshTransaction{
					State:       types.APPLIED,
					Transaction: *tx,
				})
			}
		}
	}
	return
}

func (t *ConStateAPIMock) GetLayerStateRoot(types.LayerID) (types.Hash32, error) {
	return stateRoot, nil
}

func (t *ConStateAPIMock) GetBalance(addr types.Address) (uint64, error) {
	return t.balances[addr].Uint64(), nil
}

func (t *ConStateAPIMock) GetNonce(addr types.Address) (types.Nonce, error) {
	return t.nonces[addr], nil
}

func (t *ConStateAPIMock) Validation(raw types.RawTx) system.ValidationRequest {
	panic("dont use this")
}

func NewTx(nonce uint64, recipient types.Address, signer *signing.EdSigner) *types.Transaction {
	tx := types.Transaction{TxHeader: &types.TxHeader{}}
	tx.Principal = wallet.Address(signer.PublicKey().Bytes())
	if nonce == 0 {
		tx.RawTx = types.NewRawTx(wallet.SelfSpawn(signer.PrivateKey(),
			0,
			sdk.WithGasPrice(0),
		))
	} else {
		tx.RawTx = types.NewRawTx(
			wallet.Spend(signer.PrivateKey(), recipient, 1,
				nonce,
				sdk.WithGasPrice(0),
			),
		)
		tx.MaxSpend = 1
	}
	return &tx
}

func newChallenge(sequence uint64, prevAtxID, posAtxID types.ATXID, epoch types.EpochID) types.NIPostChallenge {
	return types.NIPostChallenge{
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PublishEpoch:   epoch,
		PositioningATX: posAtxID,
	}
}

func launchServer(tb testing.TB, services ...ServiceAPI) (Config, func()) {
	cfg := DefaultTestConfig()
	grpcService, err := NewWithServices(cfg.PublicListener, zaptest.NewLogger(tb).Named("grpc"), cfg, services)
	require.NoError(tb, err)

	// start gRPC server
	require.NoError(tb, grpcService.Start())

	// update config with bound addresses
	cfg.PublicListener = grpcService.BoundAddress

	return cfg, func() { assert.NoError(tb, grpcService.Close()) }
}

func getFreePort(optionalPort int) (int, error) {
	l, e := net.Listen("tcp", fmt.Sprintf(":%v", optionalPort))
	if e != nil {
		l, e = net.Listen("tcp", ":0")
		if e != nil {
			return 0, fmt.Errorf("listen TCP: %w", e)
		}
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func TestNewServersConfig(t *testing.T) {
	port1, err := getFreePort(0)
	require.NoError(t, err, "Should be able to establish a connection on a port")

	port2, err := getFreePort(0)
	require.NoError(t, err, "Should be able to establish a connection on a port")

	grpcService := New(fmt.Sprintf(":%d", port1), zaptest.NewLogger(t).Named("grpc"), DefaultTestConfig())
	jsonService := NewJSONHTTPServer(fmt.Sprintf(":%d", port2), zaptest.NewLogger(t).Named("grpc.JSON"))

	require.Contains(t, grpcService.listener, strconv.Itoa(port1), "Expected same port")
	require.Contains(t, jsonService.listener, strconv.Itoa(port2), "Expected same port")
}

func TestNewLocalServer(t *testing.T) {
	tt := []struct {
		name     string
		listener string
		warn     bool
	}{
		{
			name:     "valid",
			listener: "192.168.1.1:1234",
			warn:     false,
		},
		{
			name:     "valid random port",
			listener: "10.0.0.1:0",
			warn:     false,
		},
		{
			name:     "invalid",
			listener: "0.0.0.0:1234",
			warn:     true,
		},
		{
			name:     "invalid random port",
			listener: "88.77.66.11:0",
			warn:     true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			observer, observedLogs := observer.New(zapcore.WarnLevel)
			logger := zap.New(observer)

			ctrl := gomock.NewController(t)
			peerCounter := NewMockpeerCounter(ctrl)
			meshApi := NewMockmeshAPI(ctrl)
			genTime := NewMockgenesisTimeAPI(ctrl)
			syncer := NewMocksyncer(ctrl)

			cfg := DefaultTestConfig()
			cfg.PostListener = tc.listener
			svc := NewNodeService(peerCounter, meshApi, genTime, syncer, "v0.0.0", "cafebabe")
			grpcService, err := NewWithServices(cfg.PostListener, logger, cfg, []ServiceAPI{svc})
			if tc.warn {
				require.Equal(t, 1, observedLogs.Len(), "Expected a warning log")
				require.Equal(t, observedLogs.All()[0].Message, "unsecured grpc server is listening on a public IP address")
				require.Equal(t, observedLogs.All()[0].ContextMap()["address"], tc.listener)
				return
			}

			require.NoError(t, err)
			require.Equal(t, grpcService.listener, tc.listener, "expected same listener")
		})
	}
}

type smesherServiceConn struct {
	pb.SmesherServiceClient

	smeshingProvider *activation.MockSmeshingProvider
	postSupervisor   *MockpostSupervisor
}

func setupSmesherService(t *testing.T, sig *signing.EdSigner) (*smesherServiceConn, context.Context) {
	ctrl, mockCtx := gomock.WithContext(context.Background(), t)
	smeshingProvider := activation.NewMockSmeshingProvider(ctrl)
	postSupervisor := NewMockpostSupervisor(ctrl)
	svc := NewSmesherService(
		smeshingProvider,
		postSupervisor,
		10*time.Millisecond,
		sig,
		activation.DefaultPostSetupOpts(),
	)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	client := pb.NewSmesherServiceClient(conn)

	return &smesherServiceConn{
		SmesherServiceClient: client,

		smeshingProvider: smeshingProvider,
		postSupervisor:   postSupervisor,
	}, mockCtx
}

func TestSmesherService(t *testing.T) {
	t.Run("IsSmeshing", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		c.smeshingProvider.EXPECT().Smeshing().Return(false)
		res, err := c.IsSmeshing(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.False(t, res.IsSmeshing, "expected IsSmeshing to be false")
	})

	t.Run("StartSmeshingMissingArgs", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		_, err := c.StartSmeshing(ctx, &pb.StartSmeshingRequest{})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("StartSmeshing", func(t *testing.T) {
		t.Parallel()
		opts := &pb.PostSetupOpts{}
		opts.DataDir = t.TempDir()
		opts.NumUnits = 1
		opts.MaxFileSize = 1024

		coinbase := &pb.AccountId{Address: addr1.String()}
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		c, ctx := setupSmesherService(t, sig)
		c.smeshingProvider.EXPECT().StartSmeshing(gomock.Any()).Return(nil)
		c.postSupervisor.EXPECT().Start(gomock.All(
			gomock.Cond(func(postOpts any) bool { return postOpts.(activation.PostSetupOpts).DataDir == opts.DataDir }),
			gomock.Cond(
				func(postOpts any) bool { return postOpts.(activation.PostSetupOpts).NumUnits == opts.NumUnits },
			),
			gomock.Cond(
				func(postOpts any) bool { return postOpts.(activation.PostSetupOpts).MaxFileSize == opts.MaxFileSize },
			),
		), sig).Return(nil)
		res, err := c.StartSmeshing(ctx, &pb.StartSmeshingRequest{
			Opts:     opts,
			Coinbase: coinbase,
		})
		require.NoError(t, err)
		require.Equal(t, int32(code.Code_OK), res.Status.Code)
	})

	t.Run("StartSmeshingMultiSetup", func(t *testing.T) {
		t.Parallel()
		opts := &pb.PostSetupOpts{}
		opts.DataDir = t.TempDir()
		opts.NumUnits = 1
		opts.MaxFileSize = 1024

		coinbase := &pb.AccountId{Address: addr1.String()}

		c, ctx := setupSmesherService(t, nil) // in multi smeshing setup the node id is nil and start smeshing should fail
		res, err := c.StartSmeshing(ctx, &pb.StartSmeshingRequest{
			Opts:     opts,
			Coinbase: coinbase,
		})
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
		require.ErrorContains(t, err, "node is not configured for supervised smeshing")
		require.Nil(t, res)
	})

	t.Run("StopSmeshing", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		c.smeshingProvider.EXPECT().StopSmeshing(gomock.Any()).Return(nil)
		c.postSupervisor.EXPECT().Stop(false).Return(nil)
		res, err := c.StopSmeshing(ctx, &pb.StopSmeshingRequest{})
		require.NoError(t, err)
		require.Equal(t, int32(code.Code_OK), res.Status.Code)
	})

	t.Run("SmesherIDs", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		nodeId := types.RandomNodeID()
		c.smeshingProvider.EXPECT().SmesherIDs().Return([]types.NodeID{nodeId})
		res, err := c.SmesherIDs(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.Equal(t, 1, len(res.PublicKeys))
		require.Equal(t, nodeId.Bytes(), res.PublicKeys[0])
	})

	t.Run("SetCoinbaseMissingArgs", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		_, err := c.SetCoinbase(ctx, &pb.SetCoinbaseRequest{})
		require.Error(t, err)
		statusCode := status.Code(err)
		require.Equal(t, codes.InvalidArgument, statusCode)
	})

	t.Run("SetCoinbase", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		c.smeshingProvider.EXPECT().SetCoinbase(addr1)
		res, err := c.SetCoinbase(ctx, &pb.SetCoinbaseRequest{
			Id: &pb.AccountId{Address: addr1.String()},
		})
		require.NoError(t, err)
		require.Equal(t, int32(code.Code_OK), res.Status.Code)
	})

	t.Run("Coinbase", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		c.smeshingProvider.EXPECT().Coinbase().Return(addr1)
		res, err := c.Coinbase(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		addr, err := types.StringToAddress(res.AccountId.Address)
		require.NoError(t, err)
		require.Equal(t, addr1, addr)
	})

	t.Run("MinGas", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		_, err := c.MinGas(ctx, &emptypb.Empty{})
		require.Error(t, err)
		statusCode := status.Code(err)
		require.Equal(t, codes.Unimplemented, statusCode)
	})

	t.Run("SetMinGas", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		_, err := c.SetMinGas(ctx, &pb.SetMinGasRequest{})
		require.Error(t, err)
		statusCode := status.Code(err)
		require.Equal(t, codes.Unimplemented, statusCode)
	})

	t.Run("PostSetupComputeProviders", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		c.postSupervisor.EXPECT().Providers().Return(nil, nil)
		_, err := c.PostSetupProviders(ctx, &pb.PostSetupProvidersRequest{Benchmark: false})
		require.NoError(t, err)
	})

	t.Run("PostSetupStatusStream", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupSmesherService(t, nil)
		c.postSupervisor.EXPECT().Status().Return(&activation.PostSetupStatus{}).AnyTimes()

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		stream, err := c.PostSetupStatusStream(ctx, &emptypb.Empty{})
		require.NoError(t, err)

		// Expecting the stream to return updates before closing.
		for i := 0; i < 3; i++ {
			_, err = stream.Recv()
			require.NoError(t, err)
		}

		cancel()
		_, err = stream.Recv()
		require.ErrorContains(t, err, context.Canceled.Error())
	})
}

func TestMeshService(t *testing.T) {
	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	genesis := time.Unix(genTimeUnix, 0)
	genTime.EXPECT().GenesisTime().Return(genesis)
	genTime.EXPECT().CurrentLayer().Return(layerCurrent).AnyTimes()
	db := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	svc := NewMeshService(
		db,
		meshAPIMock,
		conStateAPI,
		genTime,
		layersPerEpoch,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	require.NoError(
		t,
		activesets.Add(
			db,
			ballot1.EpochData.ActiveSetHash,
			&types.EpochActiveSet{Set: types.ATXIDList{globalAtx.ID(), globalAtx2.ID()}},
		),
	)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewMeshServiceClient(conn)

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{"GenesisTime", func(t *testing.T) {
			response, err := c.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(genesis.Unix()), response.Unixtime.Value)
		}},
		{"CurrentLayer", func(t *testing.T) {
			response, err := c.CurrentLayer(context.Background(), &pb.CurrentLayerRequest{})
			require.NoError(t, err)
			require.Equal(t, layerCurrent.Uint32(), response.Layernum.Number)
		}},
		{"CurrentEpoch", func(t *testing.T) {
			response, err := c.CurrentEpoch(context.Background(), &pb.CurrentEpochRequest{})
			require.NoError(t, err)
			require.Equal(t, layerCurrent.GetEpoch().Uint32(), response.Epochnum.Number)
		}},
		{"GenesisID", func(t *testing.T) {
			response, err := c.GenesisID(context.Background(), &pb.GenesisIDRequest{})
			require.NoError(t, err)
			require.Equal(t, genesisID.Bytes(), response.GenesisId)
		}},
		{"LayerDuration", func(t *testing.T) {
			response, err := c.LayerDuration(context.Background(), &pb.LayerDurationRequest{})
			require.NoError(t, err)
			require.Equal(t, layerDuration, time.Duration(response.Duration.Value)*time.Second)
		}},
		{"MaxTransactionsPerSecond", func(t *testing.T) {
			response, err := c.MaxTransactionsPerSecond(context.Background(), &pb.MaxTransactionsPerSecondRequest{})
			require.NoError(t, err)
			require.Equal(
				t,
				uint64(layerAvgSize*txsPerProposal/layerDuration.Seconds()),
				response.MaxTxsPerSecond.Value,
			)
		}},
		{"AccountMeshDataQuery", func(t *testing.T) {
			subtests := []struct {
				name string
				run  func(*testing.T)
			}{
				{
					// all inputs default to zero, no filter
					// query is valid but MaxResults is 0 so expect no results
					name: "no_inputs",
					run: func(t *testing.T) {
						_, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{})
						require.Error(t, err, "expected an error")
						require.Contains(t, err.Error(), "`Filter` must be provided")
						statusCode := status.Code(err)
						require.Equal(t, codes.InvalidArgument, statusCode)
					},
				},
				{
					name: "MinLayer_too_high",
					run: func(t *testing.T) {
						_, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MinLayer: &pb.LayerNumber{Number: layerCurrent.Add(1).Uint32()},
						})
						require.Error(t, err, "expected an error")
						require.Contains(t, err.Error(), "`LatestLayer` must be less than")
						statusCode := status.Code(err)
						require.Equal(t, codes.InvalidArgument, statusCode)
					},
				},
				{
					// This does not produce an error but we expect no results
					name: "Offset_too_high",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{
									Address: types.GenerateAddress(make([]byte, types.AddressLength)).String(),
								},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS),
							},
							Offset: math.MaxUint32,
						})
						require.NoError(t, err)
						require.Equal(t, uint32(0), res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "no_filter",
					run: func(t *testing.T) {
						_, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
						})
						require.Error(t, err, "expected an error")
						require.Contains(t, err.Error(), "`Filter` must be provided")
						statusCode := status.Code(err)
						require.Equal(t, codes.InvalidArgument, statusCode)
					},
				},
				{
					name: "empty_filter",
					run: func(t *testing.T) {
						_, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter:     &pb.AccountMeshDataFilter{},
						})
						require.Error(t, err, "expected an error")
						require.Contains(t, err.Error(), "`Filter.AccountId` must be provided")
						statusCode := status.Code(err)
						require.Equal(t, codes.InvalidArgument, statusCode)
					},
				},
				{
					name: "filter_with_empty_AccountId",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{
									Address: types.GenerateAddress(make([]byte, types.AddressLength)).String(),
								},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(0), res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "filter_with_valid_AccountId",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS),
								AccountId:            &pb.AccountId{Address: addr1.String()},
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(0), res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "filter_with_valid_AccountId_and_AccountMeshDataFlags_zero",
					run: func(t *testing.T) {
						_, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: addr1.String()},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_UNSPECIFIED),
							},
						})
						require.Error(t, err, "expected an error")
						require.Contains(t, err.Error(), "`Filter.AccountMeshDataFlags` must set at least one bitfield")
						statusCode := status.Code(err)
						require.Equal(t, codes.InvalidArgument, statusCode)
					},
				},
				{
					name: "filter_with_valid_AccountId_and_AccountMeshDataFlags_tx_only",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{Address: addr1.String()},
								AccountMeshDataFlags: uint32(
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS,
								),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(1), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
						checkAccountMeshDataItemTx(t, res.Data[0].Datum)
					},
				},
				{
					name: "filter_with_valid_AccountId_and_AccountMeshDataFlags_activations_only",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: addr1.String()},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(0), res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "filter_with_valid_AccountId_and_AccountMeshDataFlags_all",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							// Zero means unlimited
							MaxResults: uint32(0),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{Address: addr1.String()},
								AccountMeshDataFlags: uint32(
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
										pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(1), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
						checkAccountMeshDataItemTx(t, res.Data[0].Datum)
					},
				},
				{
					name: "max_results",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(1),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{Address: addr1.String()},
								AccountMeshDataFlags: uint32(
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
										pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(1), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
						checkAccountMeshDataItemTx(t, res.Data[0].Datum)
					},
				},
				{
					name: "max_results_page_2",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(1),
							Offset:     uint32(1),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{Address: addr1.String()},
								AccountMeshDataFlags: uint32(
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
										pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(1), res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
			}

			// Run sub-subtests
			for _, r := range subtests {
				t.Run(r.name, r.run)
			}
		}},
		{name: "AccountMeshDataStream", run: func(t *testing.T) {
			// common testing framework
			generateRunFn := func(req *pb.AccountMeshDataStreamRequest) func(*testing.T) {
				return func(*testing.T) {
					// Just try opening and immediately closing the stream
					stream, err := c.AccountMeshDataStream(context.Background(), req)
					require.NoError(t, err, "unexpected error opening stream")

					// Do we need this? It doesn't seem to cause any harm
					stream.Context().Done()
				}
			}
			generateRunFnError := func(msg string, req *pb.AccountMeshDataStreamRequest) func(*testing.T) {
				return func(t *testing.T) {
					// there should be no error opening the stream
					stream, err := c.AccountMeshDataStream(context.Background(), req)
					require.NoError(t, err, "unexpected error opening stream")

					// sending a request should generate an error
					_, err = stream.Recv()
					require.Error(t, err, "expected an error")
					require.Contains(t, err.Error(), msg, "received unexpected error")
					statusCode := status.Code(err)
					require.Equal(t, codes.InvalidArgument, statusCode, "expected InvalidArgument error")

					// Do we need this? It doesn't seem to cause any harm
					stream.Context().Done()
				}
			}
			subtests := []struct {
				name string
				run  func(*testing.T)
			}{
				// ERROR INPUTS
				// We expect these to produce errors
				{
					name: "missing_filter",
					run:  generateRunFnError("`Filter` must be provided", &pb.AccountMeshDataStreamRequest{}),
				},
				{
					name: "empty_filter",
					run: generateRunFnError("`Filter.AccountId` must be provided", &pb.AccountMeshDataStreamRequest{
						Filter: &pb.AccountMeshDataFilter{},
					}),
				},
				{
					name: "missing_address",
					run: generateRunFnError("`Filter.AccountId` must be provided", &pb.AccountMeshDataStreamRequest{
						Filter: &pb.AccountMeshDataFilter{
							AccountMeshDataFlags: uint32(
								pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
						},
					}),
				},
				{
					name: "filter_with_zero_flags",
					run: generateRunFnError(
						"`Filter.AccountMeshDataFlags` must set at least one bitfield",
						&pb.AccountMeshDataStreamRequest{
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: addr1.String()},
								AccountMeshDataFlags: uint32(0),
							},
						},
					),
				},

				// SUCCESS
				{
					name: "empty_address",
					run: generateRunFn(&pb.AccountMeshDataStreamRequest{
						Filter: &pb.AccountMeshDataFilter{
							AccountId: &pb.AccountId{},
							AccountMeshDataFlags: uint32(
								pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
						},
					}),
				},
				{
					name: "invalid_address",
					run: generateRunFn(&pb.AccountMeshDataStreamRequest{
						Filter: &pb.AccountMeshDataFilter{
							AccountId: &pb.AccountId{Address: types.GenerateAddress([]byte{'A'}).String()},
							AccountMeshDataFlags: uint32(
								pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
						},
					}),
				},
			}

			// Run sub-subtests
			for _, r := range subtests {
				t.Run(r.name, r.run)
			}
		}},
		{"LayersQuery", func(t *testing.T) {
			generateRunFn := func(numResults int, req *pb.LayersQueryRequest) func(*testing.T) {
				return func(t *testing.T) {
					res, err := c.LayersQuery(context.Background(), req)
					require.NoError(t, err, "query returned an unexpected error")
					require.Equal(t, numResults, len(res.Layer), "unexpected number of layer results")
				}
			}
			generateRunFnError := func(msg string, req *pb.LayersQueryRequest) func(*testing.T) {
				return func(t *testing.T) {
					_, err := c.LayersQuery(context.Background(), req)
					require.Error(t, err, "expected query to produce an error")
					require.Contains(t, err.Error(), msg, "expected error to contain string")
				}
			}
			requests := []struct {
				name string
				run  func(*testing.T)
			}{
				// ERROR INPUTS
				// We expect these to produce errors

				// end layer after current layer
				{
					name: "end_layer_after_current_layer",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: &pb.LayerNumber{Number: layerCurrent.Uint32()},
						EndLayer:   &pb.LayerNumber{Number: layerCurrent.Add(2).Uint32()},
					}),
				},

				// start layer after current layer
				{
					name: "start_layer_after_current_layer",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: &pb.LayerNumber{Number: layerCurrent.Add(2).Uint32()},
						EndLayer:   &pb.LayerNumber{Number: layerCurrent.Add(3).Uint32()},
					}),
				},

				// layer after last received
				{
					name: "layer_after_last_received",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: &pb.LayerNumber{Number: layerLatest.Add(1).Uint32()},
						EndLayer:   &pb.LayerNumber{Number: layerLatest.Add(2).Uint32()},
					}),
				},

				// very very large range
				{
					name: "very_very_large_range",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: &pb.LayerNumber{Number: 0},
						EndLayer:   &pb.LayerNumber{Number: uint32(math.MaxUint32)},
					}),
				},

				// GOOD INPUTS

				// nil inputs
				// not an error since these default to zero, see
				// https://github.com/spacemeshos/api/issues/87
				{
					name: "nil_inputs",
					run:  generateRunFn(1, &pb.LayersQueryRequest{}),
				},

				// start layer after end layer: expect no error, zero results
				{
					name: "start_layer_after_end_layer",
					run: generateRunFn(0, &pb.LayersQueryRequest{
						StartLayer: &pb.LayerNumber{Number: layerCurrent.Add(1).Uint32()},
						EndLayer:   &pb.LayerNumber{Number: layerCurrent.Uint32()},
					}),
				},

				// same start/end layer: expect no error, one result
				{
					name: "same_start_end_layer",
					run: generateRunFn(1, &pb.LayersQueryRequest{
						StartLayer: &pb.LayerNumber{Number: layerVerified.Uint32()},
						EndLayer:   &pb.LayerNumber{Number: layerVerified.Uint32()},
					}),
				},

				// start layer after last approved/confirmed layer (but before current layer)
				{
					name: "start_layer_after_last_approved_confirmed_layer",
					run: generateRunFn(2, &pb.LayersQueryRequest{
						StartLayer: &pb.LayerNumber{Number: layerVerified.Add(1).Uint32()},
						EndLayer:   &pb.LayerNumber{Number: layerVerified.Add(2).Uint32()},
					}),
				},

				// end layer after last approved/confirmed layer (but before current layer)
				{
					name: "end_layer_after_last_approved_confirmed_layer",
					// expect difference + 1 return layers
					run: generateRunFn(
						int(layerVerified.Add(2).Sub(layerFirst.Uint32()).Add(1).Uint32()),
						&pb.LayersQueryRequest{
							StartLayer: &pb.LayerNumber{Number: layerFirst.Uint32()},
							EndLayer:   &pb.LayerNumber{Number: layerVerified.Add(2).Uint32()},
						},
					),
				},

				// comprehensive valid test
				{
					name: "comprehensive",
					run: func(t *testing.T) {
						req := &pb.LayersQueryRequest{
							StartLayer: &pb.LayerNumber{Number: layerFirst.Uint32()},
							EndLayer:   &pb.LayerNumber{Number: layerLatest.Uint32()},
						}

						res, err := c.LayersQuery(context.Background(), req)
						require.NoError(t, err, "query returned unexpected error")

						// endpoint inclusive so add one
						numLayers := layerLatest.Difference(layerFirst) + 1
						require.EqualValues(t, numLayers, len(res.Layer))
						checkLayer(t, res.Layer[0])

						resLayerNine := res.Layer[9]
						require.Equal(t, uint32(9), resLayerNine.Number.Number, "layer nine is ninth")
						require.NotEmpty(t, resLayerNine.Hash)
						require.Equal(
							t,
							pb.Layer_LAYER_STATUS_UNSPECIFIED,
							resLayerNine.Status,
							"later layer is unconfirmed",
						)
					},
				},
			}

			// Run sub-subtests
			for _, r := range requests {
				t.Run(r.name, r.run)
			}
		}},
		// NOTE: There are no simple error tests for LayerStream, as it does not take any arguments.
		// See TestLayerStream_comprehensive test, below.
	}

	// Run subtests
	for _, tc := range testCases {
		t.Run(tc.name, tc.run)
	}
}

func TestTransactionServiceSubmitUnsync(t *testing.T) {
	req := require.New(t)

	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(false)
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	txHandler := NewMocktxValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(nil)

	svc := NewTransactionService(sql.InMemory(), publisher, meshAPIMock, conStateAPI, syncer, txHandler)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewTransactionServiceClient(conn)

	serializedTx, err := codec.Encode(globalTx)
	req.NoError(err, "error serializing tx")

	// This time, we expect an error, since isSynced is false (by default)
	// The node should not allow tx submission when not synced
	res, err := c.SubmitTransaction(ctx, &pb.SubmitTransactionRequest{Transaction: serializedTx})
	req.Error(err)
	grpcStatus, ok := status.FromError(err)
	req.True(ok)
	req.Equal(codes.FailedPrecondition, grpcStatus.Code())
	req.Equal("Cannot submit transaction, node is not in sync yet, try again later", grpcStatus.Message())
	req.Nil(res)

	syncer.EXPECT().IsSynced(gomock.Any()).Return(true)

	// This time, we expect no error, since isSynced is now true
	_, err = c.SubmitTransaction(ctx, &pb.SubmitTransactionRequest{Transaction: serializedTx})
	req.NoError(err)
}

func TestTransactionServiceSubmitInvalidTx(t *testing.T) {
	req := require.New(t)

	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(true)
	publisher := pubsubmocks.NewMockPublisher(ctrl) // publish is not called
	txHandler := NewMocktxValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(errors.New("failed validation"))

	grpcService := NewTransactionService(sql.InMemory(), publisher, meshAPIMock, conStateAPI, syncer, txHandler)
	cfg, cleanup := launchServer(t, grpcService)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewTransactionServiceClient(conn)

	serializedTx, err := codec.Encode(globalTx)
	req.NoError(err, "error serializing tx")

	// When verifying and caching the transaction fails we expect an error
	res, err := c.SubmitTransaction(ctx, &pb.SubmitTransactionRequest{Transaction: serializedTx})
	req.Error(err)
	grpcStatus, ok := status.FromError(err)
	req.True(ok)
	req.Equal(codes.InvalidArgument, grpcStatus.Code())
	req.Contains(grpcStatus.Message(), "Failed to verify transaction")
	req.Nil(res)
}

func TestTransactionService_SubmitNoConcurrency(t *testing.T) {
	numTxs := 20

	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(true).Times(numTxs)
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(numTxs)
	txHandler := NewMocktxValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(nil).Times(numTxs)

	grpcService := NewTransactionService(sql.InMemory(), publisher, meshAPIMock, conStateAPI, syncer, txHandler)
	cfg, cleanup := launchServer(t, grpcService)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewTransactionServiceClient(conn)
	for i := 0; i < numTxs; i++ {
		res, err := c.SubmitTransaction(ctx, &pb.SubmitTransactionRequest{
			Transaction: globalTx.Raw,
		})
		require.NoError(t, err)
		require.Equal(t, int32(code.Code_OK), res.Status.Code)
		require.Equal(t, globalTx.ID.Bytes(), res.Txstate.Id.Id)
		require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res.Txstate.State)
	}
}

func TestTransactionService(t *testing.T) {
	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	txHandler := NewMocktxValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	grpcService := NewTransactionService(sql.InMemory(), publisher, meshAPIMock, conStateAPI, syncer, txHandler)
	cfg, cleanup := launchServer(t, grpcService)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewTransactionServiceClient(conn)

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{"SubmitSpawnTransaction", func(t *testing.T) {
			res, err := c.SubmitTransaction(context.Background(), &pb.SubmitTransactionRequest{
				Transaction: globalTx.Raw,
			})
			require.NoError(t, err)
			require.Equal(t, int32(code.Code_OK), res.Status.Code)
			require.Equal(t, globalTx.ID.Bytes(), res.Txstate.Id.Id)
			require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res.Txstate.State)
		}},
		{"TransactionsState_MissingTransactionId", func(t *testing.T) {
			_, err := c.TransactionsState(context.Background(), &pb.TransactionsStateRequest{})
			statusCode := status.Code(err)
			require.Equal(t, codes.InvalidArgument, statusCode)
			require.Contains(t, err.Error(), "`TransactionId` must include")
		}},
		{"TransactionsState_TransactionIdZeroLen", func(t *testing.T) {
			_, err := c.TransactionsState(context.Background(), &pb.TransactionsStateRequest{
				TransactionId: []*pb.TransactionId{},
			})
			statusCode := status.Code(err)
			require.Equal(t, codes.InvalidArgument, statusCode)
			require.Contains(t, err.Error(), "`TransactionId` must include")
		}},
		{"TransactionsState_StateOnly", func(t *testing.T) {
			req := &pb.TransactionsStateRequest{}
			req.TransactionId = append(req.TransactionId, &pb.TransactionId{
				Id: globalTx.ID.Bytes(),
			})
			res, err := c.TransactionsState(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, 1, len(res.TransactionsState))
			require.Equal(t, 0, len(res.Transactions))
			require.Equal(t, globalTx.ID.Bytes(), res.TransactionsState[0].Id.Id)
			require.Equal(t, pb.TransactionState_TRANSACTION_STATE_PROCESSED, res.TransactionsState[0].State)
		}},
		{"TransactionsState_All", func(t *testing.T) {
			req := &pb.TransactionsStateRequest{}
			req.IncludeTransactions = true
			req.TransactionId = append(req.TransactionId, &pb.TransactionId{
				Id: globalTx.ID.Bytes(),
			})
			res, err := c.TransactionsState(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, 1, len(res.TransactionsState))
			require.Equal(t, 1, len(res.Transactions))
			require.Equal(t, globalTx.ID.Bytes(), res.TransactionsState[0].Id.Id)
			require.Equal(t, pb.TransactionState_TRANSACTION_STATE_PROCESSED, res.TransactionsState[0].State)

			checkTransaction(t, res.Transactions[0])
		}},
		{"TransactionsStateStream_MissingTransactionId", func(t *testing.T) {
			req := &pb.TransactionsStateStreamRequest{}
			stream, err := c.TransactionsStateStream(context.Background(), req)
			require.NoError(t, err)
			_, err = stream.Recv()
			statusCode := status.Code(err)
			require.Equal(t, codes.InvalidArgument, statusCode)
			require.Contains(t, err.Error(), "`TransactionId` must include")
		}},
		{"TransactionsStateStream_TransactionIdZeroLen", func(t *testing.T) {
			req := &pb.TransactionsStateStreamRequest{
				TransactionId: []*pb.TransactionId{},
			}
			stream, err := c.TransactionsStateStream(context.Background(), req)
			require.NoError(t, err)
			_, err = stream.Recv()
			statusCode := status.Code(err)
			require.Equal(t, codes.InvalidArgument, statusCode)
			require.Contains(t, err.Error(), "`TransactionId` must include")
		}},
		{"TransactionsStateStream_StateOnly", func(t *testing.T) {
			// Set up the reporter
			req := &pb.TransactionsStateStreamRequest{}
			req.TransactionId = append(req.TransactionId, &pb.TransactionId{
				Id: globalTx.ID.Bytes(),
			})

			events.CloseEventReporter()

			events.InitializeReporter()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stream, err := c.TransactionsStateStream(ctx, req)
			require.NoError(t, err)
			// Give the server-side time to subscribe to events
			time.Sleep(time.Millisecond * 50)

			events.ReportNewTx(0, globalTx)
			res, err := stream.Recv()
			require.NoError(t, err)
			require.Nil(t, res.Transaction)
			require.Equal(t, globalTx.ID.Bytes(), res.TransactionState.Id.Id)
			require.Equal(t, pb.TransactionState_TRANSACTION_STATE_PROCESSED, res.TransactionState.State)
		}},
		{"TransactionsStateStream_All", func(t *testing.T) {
			events.CloseEventReporter()
			events.InitializeReporter()
			t.Cleanup(events.CloseEventReporter)

			req := &pb.TransactionsStateStreamRequest{}
			req.TransactionId = append(req.TransactionId, &pb.TransactionId{
				Id: globalTx.ID.Bytes(),
			})
			req.IncludeTransactions = true

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stream, err := c.TransactionsStateStream(ctx, req)
			require.NoError(t, err)
			// Give the server-side time to subscribe to events
			time.Sleep(time.Millisecond * 50)

			events.ReportNewTx(0, globalTx)

			// Verify
			res, err := stream.Recv()
			require.NoError(t, err)
			require.Equal(t, globalTx.ID.Bytes(), res.TransactionState.Id.Id)
			require.Equal(t, pb.TransactionState_TRANSACTION_STATE_PROCESSED, res.TransactionState.State)
			checkTransaction(t, res.Transaction)
		}},
		// Submit a tx, then receive it over the stream
		{"TransactionsState_SubmitThenStream", func(t *testing.T) {
			events.CloseEventReporter()
			events.InitializeReporter()
			t.Cleanup(events.CloseEventReporter)

			// Remove the tx from the mesh so it only appears in the mempool
			delete(conStateAPI.returnTx, globalTx.ID)
			defer func() { conStateAPI.returnTx[globalTx.ID] = globalTx }()

			// STREAM
			// Open the stream first and listen for new transactions
			req := &pb.TransactionsStateStreamRequest{}
			req.TransactionId = append(req.TransactionId, &pb.TransactionId{
				Id: globalTx.ID.Bytes(),
			})
			req.IncludeTransactions = true

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Simulate the process by which a newly-broadcast tx lands in the mempool
			broadcastSignal := make(chan struct{})
			var wgBroadcast sync.WaitGroup
			wgBroadcast.Add(1)
			go func() {
				defer wgBroadcast.Done()
				select {
				case <-ctx.Done():
					require.Fail(t, "context deadline exceeded while waiting for broadcast signal")
					return
				case <-broadcastSignal:
					// We assume the data is valid here, and put it directly into the txpool
					conStateAPI.put(globalTx.ID, globalTx)
				}
			}()

			stream, err := c.TransactionsStateStream(ctx, req)
			require.NoError(t, err)
			// Give the server-side time to subscribe to events
			time.Sleep(time.Millisecond * 50)

			res, err := c.SubmitTransaction(ctx, &pb.SubmitTransactionRequest{
				Transaction: globalTx.Raw,
			})
			require.NoError(t, err)
			require.Equal(t, int32(code.Code_OK), res.Status.Code)
			require.Equal(t, globalTx.ID.Bytes(), res.Txstate.Id.Id)
			require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res.Txstate.State)
			close(broadcastSignal)
			wgBroadcast.Wait()

			response, err := stream.Recv()
			require.NoError(t, err)
			require.Equal(t, globalTx.ID.Bytes(), response.TransactionState.Id.Id)
			// We expect the tx to go to the mempool
			require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, response.TransactionState.State)
			checkTransaction(t, response.Transaction)
		}},
		{"TransactionsStateStream_ManySubscribers", func(t *testing.T) {
			events.CloseEventReporter()
			events.InitializeReporter()
			t.Cleanup(events.CloseEventReporter)

			req := &pb.TransactionsStateStreamRequest{}
			req.TransactionId = append(req.TransactionId, &pb.TransactionId{
				Id: globalTx.ID.Bytes(),
			})
			req.IncludeTransactions = true

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			const subscriberCount = 10
			streams := make([]pb.TransactionService_TransactionsStateStreamClient, 0, subscriberCount)
			for i := 0; i < subscriberCount; i++ {
				stream, err := c.TransactionsStateStream(ctx, req)
				require.NoError(t, err)
				streams = append(streams, stream)
			}
			// Give the server-side time to subscribe to events
			time.Sleep(time.Millisecond * 50)

			// TODO send header after stream has subscribed

			events.ReportNewTx(0, globalTx)

			for _, stream := range streams {
				res, err := stream.Recv()
				require.NoError(t, err)
				require.Equal(t, globalTx.ID.Bytes(), res.TransactionState.Id.Id)
				require.Equal(t, pb.TransactionState_TRANSACTION_STATE_PROCESSED, res.TransactionState.State)
				checkTransaction(t, res.Transaction)
			}
		}},
		{"TransactionsStateStream_NoEventReceiving", func(t *testing.T) {
			events.CloseEventReporter()
			events.InitializeReporter()
			t.Cleanup(events.CloseEventReporter)

			req := &pb.TransactionsStateStreamRequest{}
			req.TransactionId = append(req.TransactionId, &pb.TransactionId{
				Id: globalTx.ID.Bytes(),
			})
			req.IncludeTransactions = true

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			stream, err := c.TransactionsStateStream(ctx, req)
			require.NoError(t, err)
			// Give the server-side time to subscribe to events
			time.Sleep(time.Millisecond * 50)

			for i := 0; i < subscriptionChanBufSize*2; i++ {
				events.ReportNewTx(0, globalTx)
			}

			for i := 0; i < subscriptionChanBufSize; i++ {
				_, err := stream.Recv()
				if err != nil {
					st, ok := status.FromError(err)
					require.True(t, ok)
					require.Equal(t, st.Message(), errTxBufferFull)
				}
			}
		}},
	}

	// Run subtests
	for _, tc := range testCases {
		t.Run(tc.name, tc.run)
	}
}

func checkTransaction(t *testing.T, tx *pb.Transaction) {
	require.Equal(t, globalTx.ID.Bytes(), tx.Id)
	require.Equal(t, globalTx.Principal.String(), tx.Principal.Address)
	require.Equal(t, globalTx.GasPrice, tx.GasPrice)
	require.Equal(t, globalTx.MaxGas, tx.MaxGas)
	require.Equal(t, globalTx.MaxSpend, tx.MaxSpend)
	require.Equal(t, globalTx.Nonce, tx.Nonce.Counter)
}

func checkLayer(t *testing.T, l *pb.Layer) {
	require.Equal(t, uint32(0), l.Number.Number, "first layer is zero")
	require.Equal(t, pb.Layer_LAYER_STATUS_CONFIRMED, l.Status, "first layer is confirmed")

	require.Equal(t, atxPerLayer, len(l.Activations), "unexpected number of activations in layer")
	require.Equal(t, blkPerLayer, len(l.Blocks), "unexpected number of blocks in layer")
	require.Equal(t, stateRoot.Bytes(), l.RootStateHash, "unexpected state root")

	resBlock := l.Blocks[0]

	resTxIDs := make([]types.TransactionID, 0, len(resBlock.Transactions))
	for _, tx := range resBlock.Transactions {
		resTxIDs = append(resTxIDs, types.TransactionID(types.BytesToHash(tx.Id)))
	}
	require.ElementsMatch(t, block1.TxIDs, resTxIDs)
	require.Equal(t, types.Hash20(block1.ID()).Bytes(), resBlock.Id)

	// Check the tx as well
	resTx := resBlock.Transactions[0]
	require.Equal(t, globalTx.ID.Bytes(), resTx.Id)
	require.Equal(t, globalTx.Principal.String(), resTx.Principal.Address)
	require.Equal(t, globalTx.GasPrice, resTx.GasPrice)
	require.Equal(t, globalTx.MaxGas, resTx.MaxGas)
	require.Equal(t, globalTx.MaxSpend, resTx.MaxSpend)
	require.Equal(t, globalTx.Nonce, resTx.Nonce.Counter)
}

func TestAccountMeshDataStream_comprehensive(t *testing.T) {
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	grpcService := NewMeshService(
		datastore.NewCachedDB(sql.InMemory(), logtest.New(t)),
		meshAPIMock,
		conStateAPI,
		genTime,
		layersPerEpoch,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	cfg, cleanup := launchServer(t, grpcService)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewMeshServiceClient(conn)

	// set up the grpc listener stream
	req := &pb.AccountMeshDataStreamRequest{
		Filter: &pb.AccountMeshDataFilter{
			AccountId: &pb.AccountId{Address: addr1.String()},
			AccountMeshDataFlags: uint32(
				pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
					pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
		},
	}

	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream, err := c.AccountMeshDataStream(ctx, req)
	require.NoError(t, err, "stream request returned unexpected error")
	// Give the server-side time to subscribe to events
	time.Sleep(time.Millisecond * 50)

	// publish a tx
	events.ReportNewTx(0, globalTx)
	res, err := stream.Recv()
	require.NoError(t, err, "got error from stream")
	checkAccountMeshDataItemTx(t, res.Datum.Datum)

	// test streaming a tx and an atx that are filtered out
	// these should not be received
	events.ReportNewTx(0, globalTx2)
	events.ReportNewActivation(globalAtx2)

	_, err = stream.Recv()
	require.Error(t, err)
	require.Contains(t, []codes.Code{codes.Unknown, codes.DeadlineExceeded}, status.Convert(err).Code())
}

func TestAccountDataStream_comprehensive(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	svc := NewGlobalStateService(meshAPIMock, conStateAPI)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewGlobalStateServiceClient(conn)

	// set up the grpc listener stream
	req := &pb.AccountDataStreamRequest{
		Filter: &pb.AccountDataFilter{
			AccountId: &pb.AccountId{Address: addr1.String()},
			AccountDataFlags: uint32(
				pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_TRANSACTION_RECEIPT),
		},
	}

	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream, err := c.AccountDataStream(ctx, req)
	require.NoError(t, err, "stream request returned unexpected error")
	// Give the server-side time to subscribe to events
	time.Sleep(time.Millisecond * 50)

	events.ReportRewardReceived(types.Reward{
		Layer:       layerFirst,
		TotalReward: rewardAmount,
		LayerReward: rewardAmount * 2,
		Coinbase:    addr1,
		SmesherID:   rewardSmesherID,
	})

	res, err := stream.Recv()
	require.NoError(t, err)
	checkAccountDataItemReward(t, res.Datum.Datum)

	// publish an account data update
	events.ReportAccountUpdate(addr1)

	res, err = stream.Recv()
	require.NoError(t, err)
	checkAccountDataItemAccount(t, res.Datum.Datum)

	// test streaming a reward and account update that should be filtered out
	// these should not be received
	events.ReportAccountUpdate(addr2)
	events.ReportRewardReceived(types.Reward{Coinbase: addr2})

	_, err = stream.Recv()
	require.Error(t, err)
}

func TestGlobalStateStream_comprehensive(t *testing.T) {
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	svc := NewGlobalStateService(meshAPIMock, conStateAPI)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewGlobalStateServiceClient(conn)

	// set up the grpc listener stream
	req := &pb.GlobalStateStreamRequest{
		GlobalStateDataFlags: uint32(
			pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_ACCOUNT |
				pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_GLOBAL_STATE_HASH |
				pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_REWARD),
	}

	stream, err := c.GlobalStateStream(ctx, req)
	require.NoError(t, err, "stream request returned unexpected error")
	// Give the server-side time to subscribe to events
	time.Sleep(time.Millisecond * 50)

	// publish a reward
	events.ReportRewardReceived(types.Reward{
		Layer:       layerFirst,
		TotalReward: rewardAmount,
		LayerReward: rewardAmount * 2,
		Coinbase:    addr1,
		SmesherID:   rewardSmesherID,
	})
	res, err := stream.Recv()
	require.NoError(t, err, "got error from stream")
	checkGlobalStateDataReward(t, res.Datum.Datum)

	// publish an account data update
	events.ReportAccountUpdate(addr1)
	res, err = stream.Recv()
	require.NoError(t, err, "got error from stream")
	checkGlobalStateDataAccountWrapper(t, res.Datum.Datum)

	// publish a new layer
	layer, err := meshAPIMock.GetLayer(layerFirst)
	require.NoError(t, err)

	events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: layer.Index(),
		Status:  events.LayerStatusTypeApplied,
	})
	res, err = stream.Recv()
	require.NoError(t, err, "got error from stream")
	checkGlobalStateDataGlobalState(t, res.Datum.Datum)
}

func TestLayerStream_comprehensive(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	db := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))

	grpcService := NewMeshService(
		db,
		meshAPIMock,
		conStateAPI,
		genTime,
		layersPerEpoch,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	cfg, cleanup := launchServer(t, grpcService)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)

	// set up the grpc listener stream
	c := pb.NewMeshServiceClient(conn)
	stream, err := c.LayerStream(ctx, &pb.LayerStreamRequest{})
	require.NoError(t, err, "stream request returned unexpected error")
	// Give the server-side time to subscribe to events
	time.Sleep(time.Millisecond * 50)

	layer, err := meshAPIMock.GetLayer(layerFirst)
	require.NoError(t, err)

	// Act
	events.ReportLayerUpdate(events.LayerUpdate{
		LayerID: layer.Index(),
		Status:  events.LayerStatusTypeConfirmed,
	})

	// Verify
	res, err := stream.Recv()
	require.NoError(t, err, "got error from stream")
	require.Equal(t, uint32(0), res.Layer.Number.Number)
	require.Equal(t, events.LayerStatusTypeConfirmed, int(res.Layer.Status))
	require.NotEmpty(t, res.Layer.Hash)
	checkLayer(t, res.Layer)
}

func checkAccountDataQueryItemAccount(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.AccountData_AccountWrapper{}, dataItem)
	x := dataItem.(*pb.AccountData_AccountWrapper)
	// Check the account, nonce, and balance
	require.Equal(t, addr1.String(), x.AccountWrapper.AccountId.Address,
		"inner account has bad address")
	require.Equal(t, uint64(accountCounter), x.AccountWrapper.StateCurrent.Counter,
		"inner account has bad current counter")
	require.Equal(t, uint64(accountBalance), x.AccountWrapper.StateCurrent.Balance.Value,
		"inner account has bad current balance")
	require.Equal(t, uint64(accountCounter+1), x.AccountWrapper.StateProjected.Counter,
		"inner account has bad projected counter")
	require.Equal(t, uint64(accountBalance+1), x.AccountWrapper.StateProjected.Balance.Value,
		"inner account has bad projected balance")
}

func checkAccountDataQueryItemReward(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.AccountData_Reward{}, dataItem)
	x := dataItem.(*pb.AccountData_Reward)
	require.Equal(t, layerFirst.Uint32(), x.Reward.Layer.Number)
	require.Equal(t, uint64(rewardAmount), x.Reward.Total.Value)
	require.Equal(t, uint64(rewardAmount), x.Reward.LayerReward.Value)
	require.Equal(t, addr1.String(), x.Reward.Coinbase.Address)
	require.Equal(t, rewardSmesherID.Bytes(), x.Reward.Smesher.Id)
}

func checkAccountMeshDataItemTx(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.AccountMeshData_MeshTransaction{}, dataItem)
	x := dataItem.(*pb.AccountMeshData_MeshTransaction)
	// Check the sender
	require.Equal(t, globalTx.Principal.String(), x.MeshTransaction.Transaction.Principal.Address)
}

func checkAccountDataItemReward(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.AccountData_Reward{}, dataItem)
	x := dataItem.(*pb.AccountData_Reward)
	require.Equal(t, uint64(rewardAmount), x.Reward.Total.Value)
	require.Equal(t, layerFirst.Uint32(), x.Reward.Layer.Number)
	require.Equal(t, uint64(rewardAmount*2), x.Reward.LayerReward.Value)
	require.Equal(t, addr1.String(), x.Reward.Coinbase.Address)
	require.Equal(t, rewardSmesherID.Bytes(), x.Reward.Smesher.Id)
}

func checkAccountDataItemAccount(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.AccountData_AccountWrapper{}, dataItem)
	x := dataItem.(*pb.AccountData_AccountWrapper)
	require.Equal(t, addr1.String(), x.AccountWrapper.AccountId.Address)
	require.Equal(t, uint64(accountBalance), x.AccountWrapper.StateCurrent.Balance.Value)
	require.Equal(t, uint64(accountCounter), x.AccountWrapper.StateCurrent.Counter)
	require.Equal(t, uint64(accountBalance+1), x.AccountWrapper.StateProjected.Balance.Value)
	require.Equal(t, uint64(accountCounter+1), x.AccountWrapper.StateProjected.Counter)
}

func checkGlobalStateDataReward(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.GlobalStateData_Reward{}, dataItem)
	x := dataItem.(*pb.GlobalStateData_Reward)
	require.Equal(t, uint64(rewardAmount), x.Reward.Total.Value)
	require.Equal(t, layerFirst.Uint32(), x.Reward.Layer.Number)
	require.Equal(t, uint64(rewardAmount*2), x.Reward.LayerReward.Value)
	require.Equal(t, addr1.String(), x.Reward.Coinbase.Address)
	require.Equal(t, rewardSmesherID.Bytes(), x.Reward.Smesher.Id)
}

func checkGlobalStateDataAccountWrapper(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.GlobalStateData_AccountWrapper{}, dataItem)
	x := dataItem.(*pb.GlobalStateData_AccountWrapper)
	require.Equal(t, addr1.String(), x.AccountWrapper.AccountId.Address)
	require.Equal(t, uint64(accountBalance), x.AccountWrapper.StateCurrent.Balance.Value)
	require.Equal(t, uint64(accountCounter), x.AccountWrapper.StateCurrent.Counter)
	require.Equal(t, uint64(accountBalance+1), x.AccountWrapper.StateProjected.Balance.Value)
	require.Equal(t, uint64(accountCounter+1), x.AccountWrapper.StateProjected.Counter)
}

func checkGlobalStateDataGlobalState(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.GlobalStateData_GlobalState{}, dataItem)
	x := dataItem.(*pb.GlobalStateData_GlobalState)
	require.Equal(t, layerFirst.Uint32(), x.GlobalState.Layer.Number)
	require.Equal(t, stateRoot.Bytes(), x.GlobalState.RootHash)
}

func TestMultiService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ctrl, ctx := gomock.WithContext(ctx, t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()
	peerCounter := NewMockpeerCounter(ctrl)
	genTime := NewMockgenesisTimeAPI(ctrl)
	genesis := time.Unix(genTimeUnix, 0)
	genTime.EXPECT().GenesisTime().Return(genesis)
	svc1 := NewNodeService(peerCounter, meshAPIMock, genTime, syncer, "v0.0.0", "cafebabe")
	svc2 := NewMeshService(
		datastore.NewCachedDB(sql.InMemory(), logtest.New(t)),
		meshAPIMock,
		conStateAPI,
		genTime,
		layersPerEpoch,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	cfg, shutDown := launchServer(t, svc1, svc2)
	t.Cleanup(shutDown)

	c1 := pb.NewNodeServiceClient(dialGrpc(ctx, t, cfg))
	c2 := pb.NewMeshServiceClient(dialGrpc(ctx, t, cfg))

	// call endpoints and validate results
	const message = "Hello World"
	res1, err1 := c1.Echo(ctx, &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message},
	})
	require.NoError(t, err1)
	require.Equal(t, message, res1.Msg.Value)
	res2, err2 := c2.GenesisTime(ctx, &pb.GenesisTimeRequest{})
	require.NoError(t, err2)
	require.Equal(t, uint64(genesis.Unix()), res2.Unixtime.Value)

	// Make sure that shutting down the grpc service shuts them both down
	shutDown()

	// Make sure NodeService is off
	_, err1 = c1.Echo(ctx, &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message},
	})
	require.Error(t, err1)
	require.Contains(t, err1.Error(), "rpc error: code = Unavailable")

	// Make sure MeshService is off
	_, err2 = c2.GenesisTime(ctx, &pb.GenesisTimeRequest{})
	require.Error(t, err2)
	require.Contains(t, err2.Error(), "rpc error: code = Unavailable")
}

func TestDebugService(t *testing.T) {
	ctrl := gomock.NewController(t)
	netInfo := NewMocknetworkInfo(ctrl)
	mOracle := NewMockoracle(ctrl)
	db := sql.InMemory()

	testLog := zap.NewAtomicLevel()
	loggers := map[string]*zap.AtomicLevel{
		"test": &testLog,
	}

	svc := NewDebugService(db, conStateAPI, netInfo, mOracle, loggers)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewDebugServiceClient(conn)

	t.Run("Accounts", func(t *testing.T) {
		res, err := c.Accounts(context.Background(), &pb.AccountsRequest{})
		require.NoError(t, err)
		require.Equal(t, 2, len(res.AccountWrapper))

		// Get the list of addresses and compare them regardless of order
		var addresses []string
		for _, a := range res.AccountWrapper {
			addresses = append(addresses, a.AccountId.Address)
		}
		require.Contains(t, addresses, globalTx.Principal.String())
		require.Contains(t, addresses, addr1.String())
	})

	t.Run("Accounts at layer", func(t *testing.T) {
		lid := types.LayerID(11)
		for address, balance := range conStateAPI.balances {
			accounts.Update(db, &types.Account{
				Address:   address,
				Balance:   balance.Uint64(),
				NextNonce: conStateAPI.nonces[address],
				Layer:     lid,
			})
		}
		res, err := c.Accounts(context.Background(), &pb.AccountsRequest{Layer: lid.Uint32()})
		require.NoError(t, err)
		require.Equal(t, 2, len(res.AccountWrapper))

		// Get the list of addresses and compare them regardless of order
		var addresses []string
		for _, a := range res.AccountWrapper {
			addresses = append(addresses, a.AccountId.Address)
		}
		require.Contains(t, addresses, globalTx.Principal.String())
		require.Contains(t, addresses, addr1.String())

		_, err = c.Accounts(context.Background(), &pb.AccountsRequest{Layer: lid.Uint32() - 1})
		require.Error(t, err)
	})

	t.Run("networkID", func(t *testing.T) {
		id := p2p.Peer("test")
		netInfo.EXPECT().ID().Return(id)
		netInfo.EXPECT().ListenAddresses().Return([]ma.Multiaddr{
			mustParseMultiaddr("/ip4/0.0.0.0/tcp/5000"),
			mustParseMultiaddr("/ip4/0.0.0.0/udp/5001/quic-v1"),
		})
		netInfo.EXPECT().KnownAddresses().Return([]ma.Multiaddr{
			mustParseMultiaddr("/ip4/10.36.0.221/tcp/5000"),
			mustParseMultiaddr("/ip4/10.36.0.221/udp/5001/quic-v1"),
		})
		netInfo.EXPECT().NATDeviceType().Return(network.NATDeviceTypeCone, network.NATDeviceTypeSymmetric)
		netInfo.EXPECT().Reachability().Return(network.ReachabilityPrivate)
		netInfo.EXPECT().DHTServerEnabled().Return(true)

		response, err := c.NetworkInfo(context.Background(), &emptypb.Empty{})
		require.NoError(t, err)
		require.NotNil(t, response)
		require.Equal(t, id.String(), response.Id)
		require.Equal(t, []string{"/ip4/0.0.0.0/tcp/5000", "/ip4/0.0.0.0/udp/5001/quic-v1"},
			response.ListenAddresses)
		require.Equal(t, []string{"/ip4/10.36.0.221/tcp/5000", "/ip4/10.36.0.221/udp/5001/quic-v1"},
			response.KnownAddresses)
		require.Equal(t, pb.NetworkInfoResponse_Cone, response.NatTypeUdp)
		require.Equal(t, pb.NetworkInfoResponse_Symmetric, response.NatTypeTcp)
		require.Equal(t, pb.NetworkInfoResponse_Private, response.Reachability)
		require.True(t, response.DhtServerEnabled)
	})

	t.Run("ActiveSet", func(t *testing.T) {
		epoch := types.EpochID(3)
		activeSet := types.RandomActiveSet(11)
		mOracle.EXPECT().ActiveSet(gomock.Any(), epoch).Return(activeSet, nil)
		res, err := c.ActiveSet(context.Background(), &pb.ActiveSetRequest{
			Epoch: epoch.Uint32(),
		})
		require.NoError(t, err)
		require.Equal(t, len(activeSet), len(res.GetIds()))

		var ids []types.ATXID
		for _, a := range res.GetIds() {
			ids = append(ids, types.ATXID(types.BytesToHash(a.GetId())))
		}
		require.ElementsMatch(t, activeSet, ids)
	})
	t.Run("ProposalsStream", func(t *testing.T) {
		events.InitializeReporter()
		t.Cleanup(events.CloseEventReporter)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		stream, err := c.ProposalsStream(ctx, &emptypb.Empty{})
		require.NoError(t, err)

		_, err = stream.Header()
		require.NoError(t, err)
		events.ReportProposal(events.ProposalCreated, &types.Proposal{})
		events.ReportProposal(events.ProposalIncluded, &types.Proposal{})

		msg, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, pb.Proposal_Created, msg.Status)

		msg, err = stream.Recv()
		require.NoError(t, err)
		require.Equal(t, pb.Proposal_Included, msg.Status)
	})

	t.Run("ChangeLogLevel module debug", func(t *testing.T) {
		_, err := c.ChangeLogLevel(context.Background(), &pb.ChangeLogLevelRequest{
			Module: "test",
			Level:  "DEBUG",
		})
		require.NoError(t, err)

		require.Equal(t, testLog.Level().String(), "debug")
	})

	t.Run("ChangeLogLevel module not found", func(t *testing.T) {
		_, err := c.ChangeLogLevel(context.Background(), &pb.ChangeLogLevelRequest{
			Module: "unknown-module",
			Level:  "DEBUG",
		})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, s.Message(), "cannot find logger unknown-module")
	})

	t.Run("ChangeLogLevel unknown level", func(t *testing.T) {
		_, err := c.ChangeLogLevel(context.Background(), &pb.ChangeLogLevelRequest{
			Module: "test",
			Level:  "unknown-level",
		})
		require.Error(t, err)

		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, s.Message(), "parse level: unrecognized level: \"unknown-level\"")
	})

	t.Run("ChangeLogLevel '*' to debug", func(t *testing.T) {
		_, err := c.ChangeLogLevel(context.Background(), &pb.ChangeLogLevelRequest{
			Module: "*",
			Level:  "DEBUG",
		})
		require.NoError(t, err)

		require.Equal(t, testLog.Level().String(), "debug")
	})
}

func TestEventsReceived(t *testing.T) {
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	txService := NewTransactionService(sql.InMemory(), nil, meshAPIMock, conStateAPI, nil, nil)
	gsService := NewGlobalStateService(meshAPIMock, conStateAPI)
	cfg, cleanup := launchServer(t, txService, gsService)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn1 := dialGrpc(ctx, t, cfg)
	conn2 := dialGrpc(ctx, t, cfg)

	txClient := pb.NewTransactionServiceClient(conn1)
	accountClient := pb.NewGlobalStateServiceClient(conn2)

	txReq := &pb.TransactionsStateStreamRequest{}
	txReq.TransactionId = append(txReq.TransactionId, &pb.TransactionId{
		Id: globalTx.ID.Bytes(),
	})

	principalReq := &pb.AccountDataStreamRequest{
		Filter: &pb.AccountDataFilter{
			AccountId: &pb.AccountId{Address: addr1.String()},
			AccountDataFlags: uint32(
				pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_TRANSACTION_RECEIPT),
		},
	}

	receiverReq := &pb.AccountDataStreamRequest{
		Filter: &pb.AccountDataFilter{
			AccountId: &pb.AccountId{Address: addr2.String()},
			AccountDataFlags: uint32(
				pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_TRANSACTION_RECEIPT),
		},
	}

	txStream, err := txClient.TransactionsStateStream(ctx, txReq)
	require.NoError(t, err)

	principalStream, err := accountClient.AccountDataStream(ctx, principalReq)
	require.NoError(t, err, "stream request returned unexpected error")

	receiverStream, err := accountClient.AccountDataStream(ctx, receiverReq)
	require.NoError(t, err, "receiver stream")

	// Give the server-side time to subscribe to events
	time.Sleep(time.Millisecond * 50)

	svm := vm.New(sql.InMemory(), vm.WithLogger(logtest.New(t)))
	conState := txs.NewConservativeState(svm, sql.InMemory(), txs.WithLogger(logtest.New(t).WithName("conState")))
	conState.AddToCache(context.Background(), globalTx, time.Now())

	weight := new(big.Rat).SetFloat64(18.7)
	require.NoError(t, err)
	rewards := []types.CoinbaseReward{
		{Coinbase: addr2, Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}},
	}
	svm.Apply(vm.ApplyContext{Layer: types.GetEffectiveGenesis()},
		[]types.Transaction{*globalTx}, rewards)

	txRes, err := txStream.Recv()
	require.NoError(t, err)
	require.Nil(t, txRes.Transaction)
	require.Equal(t, globalTx.ID.Bytes(), txRes.TransactionState.Id.Id)
	require.Equal(t, pb.TransactionState_TRANSACTION_STATE_PROCESSED, txRes.TransactionState.State)

	acc1Res, err := principalStream.Recv()
	require.NoError(t, err)
	require.Equal(
		t,
		addr1.String(),
		acc1Res.Datum.Datum.(*pb.AccountData_AccountWrapper).AccountWrapper.AccountId.Address,
	)

	receiverRes, err := receiverStream.Recv()
	require.NoError(t, err)
	require.Equal(
		t,
		addr2.String(),
		receiverRes.Datum.Datum.(*pb.AccountData_AccountWrapper).AccountWrapper.AccountId.Address,
	)
}

func TestTransactionsRewards(t *testing.T) {
	req := require.New(t)
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	cfg, cleanup := launchServer(t, NewGlobalStateService(meshAPIMock, conStateAPI))
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := pb.NewGlobalStateServiceClient(dialGrpc(ctx, t, cfg))

	address := wallet.Address(types.RandomNodeID().Bytes())
	weight := new(big.Rat).SetFloat64(18.7)
	rewards := []types.CoinbaseReward{{Coinbase: address, Weight: types.RatNumFromBigRat(weight)}}

	t.Run("Get rewards from AccountDataStream", func(t *testing.T) {
		t.Parallel()
		request := &pb.AccountDataStreamRequest{
			Filter: &pb.AccountDataFilter{
				AccountId:        &pb.AccountId{Address: address.String()},
				AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD),
			},
		}
		stream, err := client.AccountDataStream(ctx, request)
		req.NoError(err, "stream request returned unexpected error")
		time.Sleep(50 * time.Millisecond)

		svm := vm.New(sql.InMemory(), vm.WithLogger(logtest.New(t)))
		_, _, err = svm.Apply(vm.ApplyContext{Layer: types.LayerID(17)}, []types.Transaction{*globalTx}, rewards)
		req.NoError(err)

		data, err := stream.Recv()
		req.NoError(err)
		req.IsType(&pb.AccountData_Reward{}, data.Datum.Datum)
		reward := data.Datum.GetReward()
		req.Equal(address.String(), reward.Coinbase.Address)
		req.EqualValues(17, reward.Layer.GetNumber())
		// TODO check reward.Total and reward.LayerReward
	})
	t.Run("Get rewards from GlobalStateStream", func(t *testing.T) {
		t.Parallel()
		request := &pb.GlobalStateStreamRequest{
			GlobalStateDataFlags: uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_REWARD),
		}
		stream, err := client.GlobalStateStream(ctx, request)
		req.NoError(err, "stream request returned unexpected error")
		time.Sleep(50 * time.Millisecond)

		svm := vm.New(sql.InMemory(), vm.WithLogger(logtest.New(t)))
		_, _, err = svm.Apply(vm.ApplyContext{Layer: types.LayerID(17)}, []types.Transaction{*globalTx}, rewards)
		req.NoError(err)

		data, err := stream.Recv()
		req.NoError(err)
		req.IsType(&pb.GlobalStateData_Reward{}, data.Datum.Datum)
		reward := data.Datum.GetReward()
		req.Equal(address.String(), reward.Coinbase.Address)
		req.EqualValues(17, reward.Layer.GetNumber())
		// TODO check reward.Total and reward.LayerReward
	})
}

func TestVMAccountUpdates(t *testing.T) {
	events.CloseEventReporter()
	events.InitializeReporter()

	// in memory database doesn't allow reads while writer locked db
	db, err := sql.Open("file:" + filepath.Join(t.TempDir(), "test.sql"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	svm := vm.New(db, vm.WithLogger(logtest.New(t)))
	cfg, cleanup := launchServer(t, NewGlobalStateService(nil, txs.NewConservativeState(svm, db)))
	t.Cleanup(cleanup)

	keys := make([]*signing.EdSigner, 10)
	accounts := make([]types.Account, len(keys))
	const initial = 100_000_000
	for i := range keys {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		keys[i] = signer
		accounts[i] = types.Account{
			Address: wallet.Address(signer.NodeID().Bytes()),
			Balance: initial,
		}
	}
	require.NoError(t, svm.ApplyGenesis(accounts))
	spawns := []types.Transaction{}
	for _, key := range keys {
		spawns = append(spawns, types.Transaction{
			RawTx: types.NewRawTx(wallet.SelfSpawn(key.PrivateKey(), 0)),
		})
	}
	lid := types.GetEffectiveGenesis().Add(1)
	_, _, err = svm.Apply(vm.ApplyContext{Layer: lid}, spawns, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := pb.NewGlobalStateServiceClient(dialGrpc(ctx, t, cfg))
	eg, ctx := errgroup.WithContext(ctx)
	states := make(chan *pb.AccountState, len(accounts))
	for _, account := range accounts {
		stream, err := client.AccountDataStream(ctx, &pb.AccountDataStreamRequest{
			Filter: &pb.AccountDataFilter{
				AccountId:        &pb.AccountId{Address: account.Address.String()},
				AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT),
			},
		})
		require.NoError(t, err)
		_, err = stream.Header()
		require.NoError(t, err)
		eg.Go(func() error {
			response, err := stream.Recv()
			if err != nil {
				return err
			}
			states <- response.Datum.GetAccountWrapper().StateCurrent
			return nil
		})
	}

	spends := []types.Transaction{}
	const amount = 100_000
	for _, key := range keys {
		spends = append(spends, types.Transaction{
			RawTx: types.NewRawTx(wallet.Spend(
				key.PrivateKey(), types.Address{1}, amount, 1,
			)),
		})
	}
	_, _, err = svm.Apply(vm.ApplyContext{Layer: lid.Add(1)}, spends, nil)
	require.NoError(t, err)
	require.NoError(t, eg.Wait())
	close(states)
	i := 0
	for state := range states {
		i++
		require.Equal(t, 2, int(state.Counter))
		require.Less(t, int(state.Balance.Value), initial-amount)
	}
	require.Equal(t, len(accounts), i)
}

func createAtxs(tb testing.TB, epoch types.EpochID, atxids []types.ATXID) []*types.VerifiedActivationTx {
	all := make([]*types.VerifiedActivationTx, 0, len(atxids))
	for _, id := range atxids {
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: epoch,
			},
			NumUnits: 1,
		}}
		atx.SetID(id)
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		atx.SmesherID = types.RandomNodeID()
		vAtx, err := atx.Verify(0, 1)
		require.NoError(tb, err)
		all = append(all, vAtx)
	}
	return all
}

func TestMeshService_EpochStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	db := sql.InMemory()
	srv := NewMeshService(
		datastore.NewCachedDB(db, logtest.New(t)),
		meshAPIMock,
		conStateAPI,
		genTime,
		layersPerEpoch,
		types.Hash20{},
		layerDuration,
		layerAvgSize,
		txsPerProposal,
	)
	cfg, cleanup := launchServer(t, srv)
	t.Cleanup(cleanup)

	epoch := types.EpochID(3)
	atxids := types.RandomActiveSet(100)
	all := createAtxs(t, epoch, atxids)
	var expected, got []types.ATXID
	for i, vatx := range all {
		require.NoError(t, atxs.Add(db, vatx))
		if i%2 == 0 {
			require.NoError(t, identities.SetMalicious(db, vatx.SmesherID, []byte("bad"), time.Now()))
		} else {
			expected = append(expected, vatx.ID())
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	client := pb.NewMeshServiceClient(conn)

	stream, err := client.EpochStream(ctx, &pb.EpochStreamRequest{Epoch: epoch.Uint32()})
	require.NoError(t, err)
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		got = append(got, types.ATXID(types.BytesToHash(resp.GetId().GetId())))
	}
	require.ElementsMatch(t, expected, got)
}

func mustParseMultiaddr(s string) ma.Multiaddr {
	maddr, err := ma.NewMultiaddr(s)
	if err != nil {
		panic("can't parse multiaddr: " + err.Error())
	}
	return maddr
}
