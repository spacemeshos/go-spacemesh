package grpcserver

//lint:file-ignore SA1019 hide deprecated protobuf version error
import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/txs"
)

const (
	labelsPerUnit  = 2048
	bitsPerLabel   = 8
	numUnits       = 2
	genTimeUnix    = 1000000
	layerDuration  = 10 * time.Second
	layerAvgSize   = 10
	txsPerProposal = 99
	layersPerEpoch = uint32(5)

	atxPerLayer    = 2
	blkPerLayer    = 3
	accountBalance = 8675301
	accountCounter = 0
	rewardAmount   = 5551234
	activesetSize  = 10001
)

var (
	txReturnLayer    = types.LayerID(1)
	layerFirst       = types.LayerID(0)
	layerVerified    = types.LayerID(8)
	layerLatest      = types.LayerID(10)
	layerCurrent     = types.LayerID(12)
	postGenesisEpoch = types.EpochID(2)
	genesisID        = types.Hash20{}

	addr1       types.Address
	addr2       types.Address
	prevAtxID   = types.ATXID(types.HexToHash32("44444"))
	chlng       = types.HexToHash32("55555")
	poetRef     = []byte("66666")
	nipost      = newNIPostWithChallenge(&chlng, poetRef)
	challenge   = newChallenge(1, prevAtxID, prevAtxID, postGenesisEpoch)
	globalAtx   *types.VerifiedActivationTx
	globalAtx2  *types.VerifiedActivationTx
	signer      *signing.EdSigner
	signer1     *signing.EdSigner
	signer2     *signing.EdSigner
	globalTx    *types.Transaction
	globalTx2   *types.Transaction
	ballot1     = genLayerBallot(types.LayerID(11))
	block1      = genLayerBlock(types.LayerID(11), nil)
	block2      = genLayerBlock(types.LayerID(11), nil)
	block3      = genLayerBlock(types.LayerID(11), nil)
	meshAPIMock = &MeshAPIMock{}
	conStateAPI = &ConStateAPIMock{
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

func dialGrpc(ctx context.Context, tb testing.TB, address string) *grpc.ClientConn {
	tb.Helper()
	conn, err := grpc.DialContext(ctx,
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(tb, err)
	tb.Cleanup(func() { require.NoError(tb, conn.Close()) })
	return conn
}

func newEdSigner(t *testing.T) *signing.EdSigner {
	t.Helper()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	return signer
}

func newAddress(t *testing.T) types.Address {
	t.Helper()
	return wallet.Address(newEdSigner(t).PublicKey().Bytes())
}

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	// run on a random port
	cfg.PublicListener = fmt.Sprintf("127.0.0.1:%d", 1024+rand.Intn(9999))
	cfg.PrivateListener = fmt.Sprintf("127.0.0.1:%d", 1024+rand.Intn(9999))

	var err error
	signer, err = signing.NewEdSigner()
	if err != nil {
		log.Println("failed to create signer:", err)
		os.Exit(1)
	}
	signer1, err = signing.NewEdSigner()
	if err != nil {
		log.Println("failed to create signer:", err)
		os.Exit(1)
	}
	signer2, err = signing.NewEdSigner()
	if err != nil {
		log.Println("failed to create signer:", err)
		os.Exit(1)
	}

	addr1 = wallet.Address(signer1.PublicKey().Bytes())
	addr2 = wallet.Address(signer2.PublicKey().Bytes())

	atx := types.NewActivationTx(challenge, addr1, nipost, numUnits, nil, nil)
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

	atx2 := types.NewActivationTx(challenge, addr2, nipost, numUnits, nil, nil)
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
	ballot1.EpochData = &types.EpochData{}
	ballot1.ActiveSet = []types.ATXID{globalAtx.ID(), globalAtx2.ID()}

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

func (m *MeshAPIMock) GetRewards(types.Address) (rewards []*types.Reward, err error) {
	return []*types.Reward{
		{
			Layer:       layerFirst,
			TotalReward: rewardAmount,
			LayerReward: rewardAmount,
			Coinbase:    addr1,
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

func (m *MeshAPIMock) EpochAtxs(types.EpochID) ([]types.ATXID, error) {
	return types.RandomActiveSet(activesetSize), nil
}

func (m *MeshAPIMock) GetATXs(context.Context, []types.ATXID) (map[types.ATXID]*types.VerifiedActivationTx, []types.ATXID) {
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

func (t *ConStateAPIMock) GetTransactionsByAddress(from, to types.LayerID, account types.Address) ([]*types.MeshTransaction, error) {
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

func (t *ConStateAPIMock) GetMeshTransactions(txIds []types.TransactionID) (txs []*types.MeshTransaction, missing map[types.TransactionID]struct{}) {
	for _, txId := range txIds {
		for _, tx := range t.returnTx {
			if tx.ID == txId {
				txs = append(txs, &types.MeshTransaction{Transaction: *tx})
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

// SmeshingAPIMock is a mock for Smeshing API.
type SmeshingAPIMock struct {
	UpdatePoETErr error
}

func (s *SmeshingAPIMock) UpdatePoETServers(context.Context, []string) error {
	return s.UpdatePoETErr
}

func (*SmeshingAPIMock) Smeshing() bool {
	return false
}

func (*SmeshingAPIMock) StartSmeshing(types.Address, activation.PostSetupOpts) error {
	return nil
}

func (*SmeshingAPIMock) StopSmeshing(bool) error {
	return nil
}

func (*SmeshingAPIMock) SmesherID() types.NodeID {
	return signer.NodeID()
}

func (*SmeshingAPIMock) Coinbase() types.Address {
	return addr1
}

func (*SmeshingAPIMock) SetCoinbase(coinbase types.Address) {
}

func marshalProto(t *testing.T, msg proto.Message) string {
	var buf bytes.Buffer
	var m jsonpb.Marshaler
	require.NoError(t, m.Marshal(&buf, msg))
	return buf.String()
}

var cfg = DefaultTestConfig()

func launchServer(tb testing.TB, cfg Config, services ...ServiceAPI) func() {
	grpcService := New(cfg.PublicListener)
	jsonService := NewJSONHTTPServer(cfg.JSONListener)

	// attach services
	for _, svc := range services {
		svc.RegisterService(grpcService)
	}

	// start gRPC and json servers
	grpcStarted := grpcService.Start()
	jsonStarted := jsonService.StartService(context.Background(), services...)

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	// wait for server to be ready (critical on CI)
	for _, ch := range []<-chan struct{}{grpcStarted, jsonStarted} {
		select {
		case <-ch:
		case <-timer.C:
		}
	}

	return func() {
		require.NoError(tb, jsonService.Shutdown(context.Background()))
		_ = grpcService.Close()
	}
}

func callEndpoint(t *testing.T, endpoint, payload string) (string, int) {
	url := fmt.Sprintf("http://%s/%s", cfg.JSONListener, endpoint)
	resp, err := http.Post(url, "application/json", strings.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return string(buf), resp.StatusCode
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
	logtest.SetupGlobal(t)
	port1, err := getFreePort(0)
	require.NoError(t, err, "Should be able to establish a connection on a port")

	port2, err := getFreePort(0)
	require.NoError(t, err, "Should be able to establish a connection on a port")

	grpcService := New(fmt.Sprintf(":%d", port1))
	jsonService := NewJSONHTTPServer(fmt.Sprintf(":%d", port2))

	require.Contains(t, grpcService.Listener, strconv.Itoa(port1), "Expected same port")
	require.Contains(t, jsonService.listener, strconv.Itoa(port2), "Expected same port")
}

func TestNodeService(t *testing.T) {
	logtest.SetupGlobal(t)

	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()
	peerCounter := NewMockpeerCounter(ctrl)
	peerCounter.EXPECT().PeerCount().Return(uint64(0)).AnyTimes()
	genTime := NewMockgenesisTimeAPI(ctrl)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	version := "v0.0.0"
	build := "cafebabe"
	grpcService := NewNodeService(ctx, peerCounter, meshAPIMock, genTime, syncer, version, build)
	t.Cleanup(launchServer(t, cfg, grpcService))

	conn := dialGrpc(ctx, t, cfg.PublicListener)
	c := pb.NewNodeServiceClient(conn)

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"Echo", func(t *testing.T) {
			logtest.SetupGlobal(t)
			const message = "Hello World"
			res, err := c.Echo(context.Background(), &pb.EchoRequest{
				Msg: &pb.SimpleString{Value: message},
			})
			require.NoError(t, err)
			require.Equal(t, message, res.Msg.Value)

			// now try sending bad payloads
			_, err = c.Echo(context.Background(), &pb.EchoRequest{Msg: nil})
			require.Error(t, err)
			grpcStatus, ok := status.FromError(err)
			require.True(t, ok)
			require.Equal(t, codes.InvalidArgument, grpcStatus.Code())
			require.Equal(t, "Must include `Msg`", grpcStatus.Message())

			_, err = c.Echo(context.Background(), &pb.EchoRequest{})
			require.Error(t, err)
			grpcStatus, ok = status.FromError(err)
			require.True(t, ok)
			require.Equal(t, codes.InvalidArgument, grpcStatus.Code())
			require.Equal(t, "Must include `Msg`", grpcStatus.Message())
		}},
		{"Version", func(t *testing.T) {
			logtest.SetupGlobal(t)
			res, err := c.Version(context.Background(), &empty.Empty{})
			require.NoError(t, err)
			require.Equal(t, version, res.VersionString.Value)
		}},
		{"Build", func(t *testing.T) {
			logtest.SetupGlobal(t)
			res, err := c.Build(context.Background(), &empty.Empty{})
			require.NoError(t, err)
			require.Equal(t, build, res.BuildString.Value)
		}},
		{"Status", func(t *testing.T) {
			logtest.SetupGlobal(t)
			// First do a mock checking during a genesis layer
			// During genesis all layers should be set to current layer

			layerCurrent := types.LayerID(layersPerEpoch) // end of first epoch
			genTime.EXPECT().CurrentLayer().Return(layerCurrent)
			req := &pb.StatusRequest{}
			res, err := c.Status(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, uint64(0), res.Status.ConnectedPeers)
			require.Equal(t, false, res.Status.IsSynced)
			require.Equal(t, layerLatest.Uint32(), res.Status.SyncedLayer.Number)
			require.Equal(t, layerCurrent.Uint32(), res.Status.TopLayer.Number)
			require.Equal(t, layerLatest.Uint32(), res.Status.VerifiedLayer.Number)

			// Now do a mock check post-genesis
			layerCurrent = types.LayerID(12)
			genTime.EXPECT().CurrentLayer().Return(layerCurrent)
			res, err = c.Status(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, uint64(0), res.Status.ConnectedPeers)
			require.Equal(t, false, res.Status.IsSynced)
			require.Equal(t, layerLatest.Uint32(), res.Status.SyncedLayer.Number)
			require.Equal(t, layerCurrent.Uint32(), res.Status.TopLayer.Number)
			require.Equal(t, layerVerified.Uint32(), res.Status.VerifiedLayer.Number)
		}},
		// NOTE: ErrorStream and StatusStream have comprehensive, E2E tests in cmd/node/node_test.go.
	}

	for _, tc := range testCases {
		t.Run(tc.name, tc.run)
	}
}

func TestGlobalStateService(t *testing.T) {
	logtest.SetupGlobal(t)
	svc := NewGlobalStateService(meshAPIMock, conStateAPI)
	t.Cleanup(launchServer(t, cfg, svc))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
	c := pb.NewGlobalStateServiceClient(conn)

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{"GlobalStateHash", func(t *testing.T) {
			logtest.SetupGlobal(t)
			res, err := c.GlobalStateHash(context.Background(), &pb.GlobalStateHashRequest{})
			require.NoError(t, err)
			require.Equal(t, layerVerified.Uint32(), res.Response.Layer.Number)
			require.Equal(t, stateRoot.Bytes(), res.Response.RootHash)
		}},
		{"Account", func(t *testing.T) {
			logtest.SetupGlobal(t)
			res, err := c.Account(context.Background(), &pb.AccountRequest{
				AccountId: &pb.AccountId{Address: addr1.String()},
			})
			require.NoError(t, err)
			require.Equal(t, addr1.String(), res.AccountWrapper.AccountId.Address)
			require.Equal(t, uint64(accountBalance), res.AccountWrapper.StateCurrent.Balance.Value)
			require.Equal(t, uint64(accountCounter), res.AccountWrapper.StateCurrent.Counter)
			require.Equal(t, uint64(accountBalance+1), res.AccountWrapper.StateProjected.Balance.Value)
			require.Equal(t, uint64(accountCounter+1), res.AccountWrapper.StateProjected.Counter)
		}},
		{"AccountDataQuery_MissingFilter", func(t *testing.T) {
			logtest.SetupGlobal(t)
			_, err := c.AccountDataQuery(context.Background(), &pb.AccountDataQueryRequest{})
			require.Error(t, err)
			require.Contains(t, err.Error(), "`Filter` must be provided")
		}},
		{"AccountDataQuery_MissingFlags", func(t *testing.T) {
			logtest.SetupGlobal(t)
			_, err := c.AccountDataQuery(context.Background(), &pb.AccountDataQueryRequest{
				Filter: &pb.AccountDataFilter{
					AccountId: &pb.AccountId{Address: addr1.String()},
				},
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "`Filter.AccountMeshDataFlags` must set at least one")
		}},
		{"AccountDataQuery_BadOffset", func(t *testing.T) {
			logtest.SetupGlobal(t)
			res, err := c.AccountDataQuery(context.Background(), &pb.AccountDataQueryRequest{
				MaxResults: uint32(1),
				Offset:     math.MaxUint32,
				Filter: &pb.AccountDataFilter{
					AccountId: &pb.AccountId{Address: addr1.String()},
					AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
						pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD),
				},
			})
			// huge offset is not an error, we just expect no results
			require.NoError(t, err)
			require.Equal(t, uint32(0), res.TotalResults)
			require.Equal(t, 0, len(res.AccountItem))
		}},
		{"AccountDataQuery_ZeroMaxResults", func(t *testing.T) {
			logtest.SetupGlobal(t)
			res, err := c.AccountDataQuery(context.Background(), &pb.AccountDataQueryRequest{
				MaxResults: uint32(0),
				Filter: &pb.AccountDataFilter{
					AccountId: &pb.AccountId{Address: addr1.String()},
					AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
						pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD),
				},
			})
			// zero maxresults means return everything
			require.NoError(t, err)
			require.Equal(t, uint32(2), res.TotalResults)
			require.Equal(t, 2, len(res.AccountItem))
		}},
		{"AccountDataQuery_OneResult", func(t *testing.T) {
			logtest.SetupGlobal(t)
			res, err := c.AccountDataQuery(context.Background(), &pb.AccountDataQueryRequest{
				MaxResults: uint32(1),
				Filter: &pb.AccountDataFilter{
					AccountId: &pb.AccountId{Address: addr1.String()},
					AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
						pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD),
				},
			})
			require.NoError(t, err)
			require.Equal(t, uint32(2), res.TotalResults)
			require.Equal(t, 1, len(res.AccountItem))
			checkAccountDataQueryItemReward(t, res.AccountItem[0].Datum)
		}},
		{"AccountDataQuery", func(t *testing.T) {
			logtest.SetupGlobal(t)
			res, err := c.AccountDataQuery(context.Background(), &pb.AccountDataQueryRequest{
				Filter: &pb.AccountDataFilter{
					AccountId: &pb.AccountId{Address: addr1.String()},
					AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
						pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD),
				},
			})
			require.NoError(t, err)
			require.Equal(t, uint32(2), res.TotalResults)
			require.Equal(t, 2, len(res.AccountItem))
			checkAccountDataQueryItemReward(t, res.AccountItem[0].Datum)
			checkAccountDataQueryItemAccount(t, res.AccountItem[1].Datum)
		}},
		{"AppEventStream", func(t *testing.T) {
			logtest.SetupGlobal(t)
			stream, err := c.AppEventStream(context.Background(), &pb.AppEventStreamRequest{})
			// We expect to be able to open the stream but for it to fail upon the first request
			require.NoError(t, err)
			_, err = stream.Recv()
			statusCode := status.Code(err)
			require.Equal(t, codes.Unimplemented, statusCode)
		}},
		{name: "AccountDataStream", run: func(t *testing.T) {
			logtest.SetupGlobal(t)
			// common testing framework
			generateRunFn := func(req *pb.AccountDataStreamRequest) func(*testing.T) {
				return func(*testing.T) {
					// Just try opening and immediately closing the stream
					stream, err := c.AccountDataStream(context.Background(), req)
					require.NoError(t, err, "unexpected error opening stream")

					// Do we need this? It doesn't seem to cause any harm
					stream.Context().Done()
				}
			}
			generateRunFnError := func(msg string, req *pb.AccountDataStreamRequest) func(*testing.T) {
				return func(t *testing.T) {
					logtest.SetupGlobal(t)
					// there should be no error opening the stream
					stream, err := c.AccountDataStream(context.Background(), req)
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
					name: "missing filter",
					run:  generateRunFnError("`Filter` must be provided", &pb.AccountDataStreamRequest{}),
				},
				{
					name: "empty filter",
					run: generateRunFnError("`Filter.AccountId` must be provided", &pb.AccountDataStreamRequest{
						Filter: &pb.AccountDataFilter{},
					}),
				},
				{
					name: "missing address",
					run: generateRunFnError("`Filter.AccountId` must be provided", &pb.AccountDataStreamRequest{
						Filter: &pb.AccountDataFilter{
							AccountDataFlags: uint32(
								pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD |
									pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT),
						},
					}),
				},
				{
					name: "filter with zero flags",
					run: generateRunFnError("`Filter.AccountDataFlags` must set at least one bitfield", &pb.AccountDataStreamRequest{
						Filter: &pb.AccountDataFilter{
							AccountId:        &pb.AccountId{Address: addr1.String()},
							AccountDataFlags: uint32(0),
						},
					}),
				},

				// SUCCESS
				{
					name: "empty address",
					run: generateRunFn(&pb.AccountDataStreamRequest{
						Filter: &pb.AccountDataFilter{
							AccountId: &pb.AccountId{},
							AccountDataFlags: uint32(
								pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD |
									pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT),
						},
					}),
				},
				{
					name: "invalid address",
					run: generateRunFn(&pb.AccountDataStreamRequest{
						Filter: &pb.AccountDataFilter{
							AccountId: &pb.AccountId{Address: types.GenerateAddress([]byte{'A'}).String()},
							AccountDataFlags: uint32(
								pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD |
									pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT),
						},
					}),
				},
			}

			// Run sub-subtests
			for _, r := range subtests {
				t.Run(r.name, r.run)
			}
		}},
		{name: "GlobalStateStream", run: func(t *testing.T) {
			logtest.SetupGlobal(t)
			// common testing framework
			generateRunFn := func(req *pb.GlobalStateStreamRequest) func(*testing.T) {
				return func(*testing.T) {
					// Just try opening and immediately closing the stream
					stream, err := c.GlobalStateStream(context.Background(), req)
					require.NoError(t, err, "unexpected error opening stream")

					// Do we need this? It doesn't seem to cause any harm
					stream.Context().Done()
				}
			}
			generateRunFnError := func(msg string, req *pb.GlobalStateStreamRequest) func(*testing.T) {
				return func(t *testing.T) {
					logtest.SetupGlobal(t)
					// there should be no error opening the stream
					stream, err := c.GlobalStateStream(context.Background(), req)
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
					name: "zero flags",
					run: generateRunFnError("`GlobalStateDataFlags` must set at least one bitfield",
						&pb.GlobalStateStreamRequest{GlobalStateDataFlags: uint32(0)}),
				},

				// SUCCESS
				{
					name: "nonzero flags",
					run: generateRunFn(&pb.GlobalStateStreamRequest{
						GlobalStateDataFlags: uint32(pb.GlobalStateDataFlag_GLOBAL_STATE_DATA_FLAG_ACCOUNT),
					}),
				},
			}

			// Run sub-subtests
			for _, r := range subtests {
				t.Run(r.name, r.run)
			}
		}},
	}

	// Run subtests
	for _, tc := range testCases {
		t.Run(tc.name, tc.run)
	}
}

func TestSmesherService(t *testing.T) {
	logtest.SetupGlobal(t)

	ctrl := gomock.NewController(t)
	postProvider := NewMockpostSetupProvider(ctrl)
	postProvider.EXPECT().Config().Return(activation.DefaultPostConfig()).AnyTimes()
	postProvider.EXPECT().Status().Return(&activation.PostSetupStatus{}).AnyTimes()
	postProvider.EXPECT().Providers().Return(nil, nil).AnyTimes()
	smeshingAPI := &SmeshingAPIMock{}
	svc := NewSmesherService(postProvider, smeshingAPI, 10*time.Millisecond, activation.DefaultPostSetupOpts())
	t.Cleanup(launchServer(t, cfg, svc))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
	c := pb.NewSmesherServiceClient(conn)

	t.Run("IsSmeshing", func(t *testing.T) {
		logtest.SetupGlobal(t)
		res, err := c.IsSmeshing(context.Background(), &empty.Empty{})
		require.NoError(t, err)
		require.False(t, res.IsSmeshing, "expected IsSmeshing to be false")
	})

	t.Run("StartSmeshingMissingArgs", func(t *testing.T) {
		logtest.SetupGlobal(t)
		_, err := c.StartSmeshing(context.Background(), &pb.StartSmeshingRequest{})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("StartSmeshing", func(t *testing.T) {
		logtest.SetupGlobal(t)
		opts := &pb.PostSetupOpts{}
		opts.DataDir = t.TempDir()
		opts.NumUnits = 1
		opts.MaxFileSize = 1024

		coinbase := &pb.AccountId{Address: addr1.String()}

		res, err := c.StartSmeshing(context.Background(), &pb.StartSmeshingRequest{
			Opts:     opts,
			Coinbase: coinbase,
		})
		require.NoError(t, err)
		require.Equal(t, int32(code.Code_OK), res.Status.Code)
	})

	t.Run("StopSmeshing", func(t *testing.T) {
		logtest.SetupGlobal(t)
		res, err := c.StopSmeshing(context.Background(), &pb.StopSmeshingRequest{})
		require.NoError(t, err)
		require.Equal(t, int32(code.Code_OK), res.Status.Code)
	})

	t.Run("SmesherID", func(t *testing.T) {
		logtest.SetupGlobal(t)
		res, err := c.SmesherID(context.Background(), &empty.Empty{})
		require.NoError(t, err)
		nodeAddr := types.GenerateAddress(signer.NodeID().Bytes())
		resAddr, err := types.StringToAddress(res.AccountId.Address)
		require.NoError(t, err)
		require.Equal(t, nodeAddr.String(), resAddr.String())
	})

	t.Run("SetCoinbaseMissingArgs", func(t *testing.T) {
		logtest.SetupGlobal(t)
		_, err := c.SetCoinbase(context.Background(), &pb.SetCoinbaseRequest{})
		require.Error(t, err)
		statusCode := status.Code(err)
		require.Equal(t, codes.InvalidArgument, statusCode)
	})

	t.Run("SetCoinbase", func(t *testing.T) {
		logtest.SetupGlobal(t)
		res, err := c.SetCoinbase(context.Background(), &pb.SetCoinbaseRequest{
			Id: &pb.AccountId{Address: addr1.String()},
		})
		require.NoError(t, err)
		require.Equal(t, int32(code.Code_OK), res.Status.Code)
	})

	t.Run("Coinbase", func(t *testing.T) {
		logtest.SetupGlobal(t)
		res, err := c.Coinbase(context.Background(), &empty.Empty{})
		require.NoError(t, err)
		addr, err := types.StringToAddress(res.AccountId.Address)
		require.NoError(t, err)
		require.Equal(t, addr1.Bytes(), addr.Bytes())
	})

	t.Run("MinGas", func(t *testing.T) {
		logtest.SetupGlobal(t)
		_, err := c.MinGas(context.Background(), &empty.Empty{})
		require.Error(t, err)
		statusCode := status.Code(err)
		require.Equal(t, codes.Unimplemented, statusCode)
	})

	t.Run("SetMinGas", func(t *testing.T) {
		logtest.SetupGlobal(t)
		_, err := c.SetMinGas(context.Background(), &pb.SetMinGasRequest{})
		require.Error(t, err)
		statusCode := status.Code(err)
		require.Equal(t, codes.Unimplemented, statusCode)
	})

	t.Run("PostSetupComputeProviders", func(t *testing.T) {
		logtest.SetupGlobal(t)
		_, err := c.PostSetupProviders(context.Background(), &pb.PostSetupProvidersRequest{Benchmark: false})
		require.NoError(t, err)
	})

	t.Run("PostSetupStatusStream", func(t *testing.T) {
		logtest.SetupGlobal(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stream, err := c.PostSetupStatusStream(ctx, &empty.Empty{})
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
	t.Run("UpdatePoetServer", func(t *testing.T) {
		logtest.SetupGlobal(t)
		smeshingAPI.UpdatePoETErr = nil
		res, err := c.UpdatePoetServers(context.Background(), &pb.UpdatePoetServersRequest{Urls: []string{"test"}})
		require.NoError(t, err)
		require.EqualValues(t, res.Status.Code, code.Code_OK)
	})
	t.Run("UpdatePoetServerUnavailable", func(t *testing.T) {
		logtest.SetupGlobal(t)
		smeshingAPI.UpdatePoETErr = activation.ErrPoetServiceUnstable
		urls := []string{"test"}
		res, err := c.UpdatePoetServers(context.Background(), &pb.UpdatePoetServersRequest{Urls: urls})
		require.Nil(t, res)
		require.ErrorIs(t, err, status.Errorf(codes.Unavailable, "can't reach poet service (%v). retry later", smeshingAPI.UpdatePoETErr))
	})
}

func TestMeshService(t *testing.T) {
	logtest.SetupGlobal(t)
	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	genesis := time.Unix(genTimeUnix, 0)
	genTime.EXPECT().GenesisTime().Return(genesis)
	genTime.EXPECT().CurrentLayer().Return(layerCurrent).AnyTimes()
	grpcService := NewMeshService(meshAPIMock, conStateAPI, genTime, layersPerEpoch, types.Hash20{}, layerDuration, layerAvgSize, txsPerProposal)
	t.Cleanup(launchServer(t, cfg, grpcService))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
	c := pb.NewMeshServiceClient(conn)

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{"GenesisTime", func(t *testing.T) {
			logtest.SetupGlobal(t)
			response, err := c.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(genesis.Unix()), response.Unixtime.Value)
		}},
		{"CurrentLayer", func(t *testing.T) {
			logtest.SetupGlobal(t)
			response, err := c.CurrentLayer(context.Background(), &pb.CurrentLayerRequest{})
			require.NoError(t, err)
			require.Equal(t, layerCurrent.Uint32(), response.Layernum.Number)
		}},
		{"CurrentEpoch", func(t *testing.T) {
			logtest.SetupGlobal(t)
			response, err := c.CurrentEpoch(context.Background(), &pb.CurrentEpochRequest{})
			require.NoError(t, err)
			require.Equal(t, layerCurrent.GetEpoch().Uint32(), response.Epochnum.Number)
		}},
		{"GenesisID", func(t *testing.T) {
			logtest.SetupGlobal(t)
			response, err := c.GenesisID(context.Background(), &pb.GenesisIDRequest{})
			require.NoError(t, err)
			require.Equal(t, genesisID.Bytes(), response.GenesisId)
		}},
		{"LayerDuration", func(t *testing.T) {
			logtest.SetupGlobal(t)
			response, err := c.LayerDuration(context.Background(), &pb.LayerDurationRequest{})
			require.NoError(t, err)
			require.Equal(t, layerDuration, time.Duration(response.Duration.Value)*time.Second)
		}},
		{"MaxTransactionsPerSecond", func(t *testing.T) {
			logtest.SetupGlobal(t)
			response, err := c.MaxTransactionsPerSecond(context.Background(), &pb.MaxTransactionsPerSecondRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(layerAvgSize*txsPerProposal/layerDuration.Seconds()), response.MaxTxsPerSecond.Value)
		}},
		{"AccountMeshDataQuery", func(t *testing.T) {
			logtest.SetupGlobal(t)
			subtests := []struct {
				name string
				run  func(*testing.T)
			}{
				{
					// all inputs default to zero, no filter
					// query is valid but MaxResults is 0 so expect no results
					name: "no_inputs",
					run: func(t *testing.T) {
						logtest.SetupGlobal(t)
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
						logtest.SetupGlobal(t)
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
						logtest.SetupGlobal(t)
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
						logtest.SetupGlobal(t)
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
						logtest.SetupGlobal(t)
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
						logtest.SetupGlobal(t)
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
						logtest.SetupGlobal(t)
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS),
								AccountId:            &pb.AccountId{Address: addr1.String()},
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(1), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
					},
				},
				{
					name: "filter_with_valid_AccountId_and_AccountMeshDataFlags_zero",
					run: func(t *testing.T) {
						logtest.SetupGlobal(t)
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
						logtest.SetupGlobal(t)
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: addr1.String()},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
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
						logtest.SetupGlobal(t)
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: addr1.String()},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(1), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
						checkAccountMeshDataItemActivation(t, res.Data[0].Datum)
					},
				},
				{
					name: "filter_with_valid_AccountId_and_AccountMeshDataFlags_all",
					run: func(t *testing.T) {
						logtest.SetupGlobal(t)
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
						require.Equal(t, uint32(2), res.TotalResults)
						require.Equal(t, 2, len(res.Data))
						checkAccountMeshDataItemTx(t, res.Data[0].Datum)
						checkAccountMeshDataItemActivation(t, res.Data[1].Datum)
					},
				},
				{
					name: "max_results",
					run: func(t *testing.T) {
						logtest.SetupGlobal(t)
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
						require.Equal(t, uint32(2), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
						checkAccountMeshDataItemTx(t, res.Data[0].Datum)
					},
				},
				{
					name: "max_results_page_2",
					run: func(t *testing.T) {
						logtest.SetupGlobal(t)
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
						require.Equal(t, uint32(2), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
						checkAccountMeshDataItemActivation(t, res.Data[0].Datum)
					},
				},
			}

			// Run sub-subtests
			for _, r := range subtests {
				t.Run(r.name, r.run)
			}
		}},
		{name: "AccountMeshDataStream", run: func(t *testing.T) {
			logtest.SetupGlobal(t)
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
					logtest.SetupGlobal(t)
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
					run: generateRunFnError("`Filter.AccountMeshDataFlags` must set at least one bitfield", &pb.AccountMeshDataStreamRequest{
						Filter: &pb.AccountMeshDataFilter{
							AccountId:            &pb.AccountId{Address: addr1.String()},
							AccountMeshDataFlags: uint32(0),
						},
					}),
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
			logtest.SetupGlobal(t)
			generateRunFn := func(numResults int, req *pb.LayersQueryRequest) func(*testing.T) {
				return func(t *testing.T) {
					logtest.SetupGlobal(t)
					res, err := c.LayersQuery(context.Background(), req)
					require.NoError(t, err, "query returned an unexpected error")
					require.Equal(t, numResults, len(res.Layer), "unexpected number of layer results")
				}
			}
			generateRunFnError := func(msg string, req *pb.LayersQueryRequest) func(*testing.T) {
				return func(t *testing.T) {
					logtest.SetupGlobal(t)
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
					run: generateRunFn(int(layerVerified.Add(2).Sub(layerFirst.Uint32()).Add(1).Uint32()), &pb.LayersQueryRequest{
						StartLayer: &pb.LayerNumber{Number: layerFirst.Uint32()},
						EndLayer:   &pb.LayerNumber{Number: layerVerified.Add(2).Uint32()},
					}),
				},

				// comprehensive valid test
				{
					name: "comprehensive",
					run: func(t *testing.T) {
						logtest.SetupGlobal(t)
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
						require.Equal(t, pb.Layer_LAYER_STATUS_UNSPECIFIED, resLayerNine.Status, "later layer is unconfirmed")
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
	logtest.SetupGlobal(t)
	req := require.New(t)

	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(false)
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	txHandler := NewMocktxValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(nil)

	grpcService := NewTransactionService(sql.InMemory(), publisher, meshAPIMock, conStateAPI, syncer, txHandler)
	t.Cleanup(launchServer(t, cfg, grpcService))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
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
	logtest.SetupGlobal(t)
	req := require.New(t)

	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(true)
	publisher := pubsubmocks.NewMockPublisher(ctrl) // publish is not called
	txHandler := NewMocktxValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(errors.New("failed validation"))

	grpcService := NewTransactionService(sql.InMemory(), publisher, meshAPIMock, conStateAPI, syncer, txHandler)
	t.Cleanup(launchServer(t, cfg, grpcService))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
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
	logtest.SetupGlobal(t)

	numTxs := 20

	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(true).Times(numTxs)
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(numTxs)
	txHandler := NewMocktxValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(nil).Times(numTxs)

	grpcService := NewTransactionService(sql.InMemory(), publisher, meshAPIMock, conStateAPI, syncer, txHandler)
	t.Cleanup(launchServer(t, cfg, grpcService))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
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
	logtest.SetupGlobal(t)

	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	txHandler := NewMocktxValidator(ctrl)
	txHandler.EXPECT().VerifyAndCacheTx(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	grpcService := NewTransactionService(sql.InMemory(), publisher, meshAPIMock, conStateAPI, syncer, txHandler)
	t.Cleanup(launchServer(t, cfg, grpcService))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
	c := pb.NewTransactionServiceClient(conn)

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{"SubmitSpawnTransaction", func(t *testing.T) {
			logtest.SetupGlobal(t)
			res, err := c.SubmitTransaction(context.Background(), &pb.SubmitTransactionRequest{
				Transaction: globalTx.Raw,
			})
			require.NoError(t, err)
			require.Equal(t, int32(code.Code_OK), res.Status.Code)
			require.Equal(t, globalTx.ID.Bytes(), res.Txstate.Id.Id)
			require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res.Txstate.State)
		}},
		{"TransactionsState_MissingTransactionId", func(t *testing.T) {
			logtest.SetupGlobal(t)
			_, err := c.TransactionsState(context.Background(), &pb.TransactionsStateRequest{})
			statusCode := status.Code(err)
			require.Equal(t, codes.InvalidArgument, statusCode)
			require.Contains(t, err.Error(), "`TransactionId` must include")
		}},
		{"TransactionsState_TransactionIdZeroLen", func(t *testing.T) {
			logtest.SetupGlobal(t)
			_, err := c.TransactionsState(context.Background(), &pb.TransactionsStateRequest{
				TransactionId: []*pb.TransactionId{},
			})
			statusCode := status.Code(err)
			require.Equal(t, codes.InvalidArgument, statusCode)
			require.Contains(t, err.Error(), "`TransactionId` must include")
		}},
		{"TransactionsState_StateOnly", func(t *testing.T) {
			logtest.SetupGlobal(t)
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
			logtest.SetupGlobal(t)
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
			logtest.SetupGlobal(t)
			req := &pb.TransactionsStateStreamRequest{}
			stream, err := c.TransactionsStateStream(context.Background(), req)
			require.NoError(t, err)
			_, err = stream.Recv()
			statusCode := status.Code(err)
			require.Equal(t, codes.InvalidArgument, statusCode)
			require.Contains(t, err.Error(), "`TransactionId` must include")
		}},
		{"TransactionsStateStream_TransactionIdZeroLen", func(t *testing.T) {
			logtest.SetupGlobal(t)
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
			logtest.SetupGlobal(t)
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
			logtest.SetupGlobal(t)
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
			logtest.SetupGlobal(t)
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
			logtest.SetupGlobal(t)
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
			logtest.SetupGlobal(t)
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

	// The order of the activations is not deterministic since they're
	// stored in a map, and randomized each run. Check if either matches.
	require.Condition(t, func() bool {
		for _, a := range l.Activations {
			// Compare the two element by element
			if a.Layer.Number != globalAtx.PublishEpoch.Uint32() {
				continue
			}
			if !bytes.Equal(a.Id.Id, globalAtx.ID().Bytes()) {
				continue
			}
			if !bytes.Equal(a.SmesherId.Id, globalAtx.SmesherID.Bytes()) {
				continue
			}
			if a.Coinbase.Address != globalAtx.Coinbase.String() {
				continue
			}
			if !bytes.Equal(a.PrevAtx.Id, globalAtx.PrevATXID.Bytes()) {
				continue
			}
			if a.NumUnits != uint32(globalAtx.NumUnits) {
				continue
			}
			// found a match
			return true
		}
		// no match
		return false
	}, "return layer does not contain expected activation data")

	resBlock := l.Blocks[0]

	require.Equal(t, len(block1.TxIDs), len(resBlock.Transactions))
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
	logtest.SetupGlobal(t)
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	grpcService := NewMeshService(meshAPIMock, conStateAPI, genTime, layersPerEpoch, types.Hash20{}, layerDuration, layerAvgSize, txsPerProposal)
	t.Cleanup(launchServer(t, cfg, grpcService))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
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

	// publish an activation
	events.ReportNewActivation(globalAtx)
	res, err = stream.Recv()
	require.NoError(t, err, "got error from stream")
	checkAccountMeshDataItemActivation(t, res.Datum.Datum)

	// test streaming a tx and an atx that are filtered out
	// these should not be received
	events.ReportNewTx(0, globalTx2)
	events.ReportNewActivation(globalAtx2)

	_, err = stream.Recv()
	require.Error(t, err)
	require.Equal(t, status.Convert(err).Code(), codes.DeadlineExceeded)
}

func TestAccountDataStream_comprehensive(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	logtest.SetupGlobal(t)
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	svc := NewGlobalStateService(meshAPIMock, conStateAPI)
	t.Cleanup(launchServer(t, cfg, svc))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
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

	events.ReportRewardReceived(events.Reward{
		Layer:       layerFirst,
		Total:       rewardAmount,
		LayerReward: rewardAmount * 2,
		Coinbase:    addr1,
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
	events.ReportRewardReceived(events.Reward{Coinbase: addr2})

	_, err = stream.Recv()
	require.Error(t, err)
}

func TestGlobalStateStream_comprehensive(t *testing.T) {
	logtest.SetupGlobal(t)
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	svc := NewGlobalStateService(meshAPIMock, conStateAPI)
	t.Cleanup(launchServer(t, cfg, svc))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
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
	events.ReportRewardReceived(events.Reward{
		Layer:       layerFirst,
		Total:       rewardAmount,
		LayerReward: rewardAmount * 2,
		Coinbase:    addr1,
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
	logtest.SetupGlobal(t)
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	grpcService := NewMeshService(meshAPIMock, conStateAPI, genTime, layersPerEpoch, types.Hash20{}, layerDuration, layerAvgSize, txsPerProposal)
	t.Cleanup(launchServer(t, cfg, grpcService))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)

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
	require.Nil(t, x.Reward.Smesher)
}

func checkAccountMeshDataItemTx(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.AccountMeshData_MeshTransaction{}, dataItem)
	x := dataItem.(*pb.AccountMeshData_MeshTransaction)
	// Check the sender
	require.Equal(t, globalTx.Principal.String(), x.MeshTransaction.Transaction.Principal.Address)
}

func checkAccountMeshDataItemActivation(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.AccountMeshData_Activation{}, dataItem)
	x := dataItem.(*pb.AccountMeshData_Activation)
	require.Equal(t, globalAtx.ID().Bytes(), x.Activation.Id.Id)
	require.Equal(t, globalAtx.PublishEpoch.Uint32(), x.Activation.Layer.Number)
	require.Equal(t, globalAtx.SmesherID.Bytes(), x.Activation.SmesherId.Id)
	require.Equal(t, globalAtx.Coinbase.String(), x.Activation.Coinbase.Address)
	require.Equal(t, globalAtx.PrevATXID.Bytes(), x.Activation.PrevAtx.Id)
	require.Equal(t, globalAtx.NumUnits, uint32(x.Activation.NumUnits))
}

func checkAccountDataItemReward(t *testing.T, dataItem any) {
	t.Helper()
	require.IsType(t, &pb.AccountData_Reward{}, dataItem)
	x := dataItem.(*pb.AccountData_Reward)
	require.Equal(t, uint64(rewardAmount), x.Reward.Total.Value)
	require.Equal(t, layerFirst.Uint32(), x.Reward.Layer.Number)
	require.Equal(t, uint64(rewardAmount*2), x.Reward.LayerReward.Value)
	require.Equal(t, addr1.String(), x.Reward.Coinbase.Address)
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
	logtest.SetupGlobal(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ctrl, ctx := gomock.WithContext(ctx, t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()
	peerCounter := NewMockpeerCounter(ctrl)
	genTime := NewMockgenesisTimeAPI(ctrl)
	genesis := time.Unix(genTimeUnix, 0)
	genTime.EXPECT().GenesisTime().Return(genesis)
	svc1 := NewNodeService(ctx, peerCounter, meshAPIMock, genTime, syncer, "v0.0.0", "cafebabe")
	svc2 := NewMeshService(meshAPIMock, conStateAPI, genTime, layersPerEpoch, types.Hash20{}, layerDuration, layerAvgSize, txsPerProposal)
	shutDown := launchServer(t, cfg, svc1, svc2)
	t.Cleanup(shutDown)

	c1 := pb.NewNodeServiceClient(dialGrpc(ctx, t, cfg.PublicListener))
	c2 := pb.NewMeshServiceClient(dialGrpc(ctx, t, cfg.PublicListener))

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

func TestJsonApi(t *testing.T) {
	logtest.SetupGlobal(t)
	const message = "hello world!"

	// we cannot start the gateway service without enabling at least one service
	shutDown := launchServer(t, cfg)
	t.Cleanup(shutDown)
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})
	url := fmt.Sprintf("http://%s/%s", cfg.JSONListener, "v1/node/echo")
	_, err := http.Post(url, "application/json", strings.NewReader(payload))
	require.Error(t, err)
	shutDown()

	// enable services and try again
	ctrl := gomock.NewController(t)
	syncer := NewMocksyncer(ctrl)
	syncer.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()
	peerCounter := NewMockpeerCounter(ctrl)
	genTime := NewMockgenesisTimeAPI(ctrl)
	genesis := time.Unix(genTimeUnix, 0)
	genTime.EXPECT().GenesisTime().Return(genesis)
	svc1 := NewNodeService(context.Background(), peerCounter, meshAPIMock, genTime, syncer, "v0.0.0", "cafebabe")
	svc2 := NewMeshService(meshAPIMock, conStateAPI, genTime, layersPerEpoch, types.Hash20{}, layerDuration, layerAvgSize, txsPerProposal)
	t.Cleanup(launchServer(t, cfg, svc1, svc2))
	time.Sleep(time.Second)

	// generate request payload (api input params)
	payload = marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})
	respBody, respStatus := callEndpoint(t, "v1/node/echo", payload)
	require.Equal(t, http.StatusOK, respStatus)
	var msg pb.EchoResponse
	require.NoError(t, jsonpb.UnmarshalString(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)

	// Test MeshService
	respBody2, respStatus2 := callEndpoint(t, "v1/mesh/genesistime", "")
	require.Equal(t, http.StatusOK, respStatus2)
	var msg2 pb.GenesisTimeResponse
	require.NoError(t, jsonpb.UnmarshalString(respBody2, &msg2))
	require.Equal(t, uint64(genesis.Unix()), msg2.Unixtime.Value)
}

func TestDebugService(t *testing.T) {
	logtest.SetupGlobal(t)
	ctrl := gomock.NewController(t)
	identity := NewMocknetworkIdentity(ctrl)
	mOracle := NewMockoracle(ctrl)
	db := sql.InMemory()
	svc := NewDebugService(db, conStateAPI, identity, mOracle)
	t.Cleanup(launchServer(t, cfg, svc))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
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
		identity.EXPECT().ID().Return(id)

		response, err := c.NetworkInfo(context.Background(), &empty.Empty{})
		require.NoError(t, err)
		require.NotNil(t, response)
		require.Equal(t, id.String(), response.Id)
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
		stream, err := c.ProposalsStream(ctx, &empty.Empty{})
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
}

func TestEventsReceived(t *testing.T) {
	logtest.SetupGlobal(t)
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	txService := NewTransactionService(sql.InMemory(), nil, meshAPIMock, conStateAPI, nil, nil)
	gsService := NewGlobalStateService(meshAPIMock, conStateAPI)
	t.Cleanup(launchServer(t, cfg, txService, gsService))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn1 := dialGrpc(ctx, t, cfg.PublicListener)
	conn2 := dialGrpc(ctx, t, cfg.PublicListener)

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
	conState.AddToCache(context.Background(), globalTx)

	weight := new(big.Rat).SetFloat64(18.7)
	require.NoError(t, err)
	rewards := []types.CoinbaseReward{{Coinbase: addr2, Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}}}
	svm.Apply(vm.ApplyContext{Layer: types.GetEffectiveGenesis()},
		[]types.Transaction{*globalTx}, rewards)

	txRes, err := txStream.Recv()
	require.NoError(t, err)
	require.Nil(t, txRes.Transaction)
	require.Equal(t, globalTx.ID.Bytes(), txRes.TransactionState.Id.Id)
	require.Equal(t, pb.TransactionState_TRANSACTION_STATE_PROCESSED, txRes.TransactionState.State)

	acc1Res, err := principalStream.Recv()
	require.NoError(t, err)
	require.Equal(t, addr1.String(), acc1Res.Datum.Datum.(*pb.AccountData_AccountWrapper).AccountWrapper.AccountId.Address)

	receiverRes, err := receiverStream.Recv()
	require.NoError(t, err)
	require.Equal(t, addr2.String(), receiverRes.Datum.Datum.(*pb.AccountData_AccountWrapper).AccountWrapper.AccountId.Address)
}

func TestTransactionsRewards(t *testing.T) {
	logtest.SetupGlobal(t)
	req := require.New(t)
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	t.Cleanup(launchServer(t, cfg, NewGlobalStateService(meshAPIMock, conStateAPI)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := pb.NewGlobalStateServiceClient(dialGrpc(ctx, t, cfg.PublicListener))

	address := newAddress(t)
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
	logtest.SetupGlobal(t)
	events.CloseEventReporter()
	events.InitializeReporter()

	// in memory database doesn't allow reads while writer locked db
	db, err := sql.Open("file:" + filepath.Join(t.TempDir(), "test.sql"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	svm := vm.New(db, vm.WithLogger(logtest.New(t)))
	t.Cleanup(launchServer(t, cfg, NewGlobalStateService(nil, txs.NewConservativeState(svm, db))))

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
	client := pb.NewGlobalStateServiceClient(dialGrpc(ctx, t, cfg.PublicListener))
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

func TestMeshService_EpochStream(t *testing.T) {
	logtest.SetupGlobal(t)

	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	srv := NewMeshService(meshAPIMock, conStateAPI, genTime, layersPerEpoch, types.Hash20{}, layerDuration, layerAvgSize, txsPerProposal)
	t.Cleanup(launchServer(t, cfg, srv))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg.PublicListener)
	client := pb.NewMeshServiceClient(conn)

	stream, err := client.EpochStream(ctx, &pb.EpochStreamRequest{Epoch: 3})
	require.NoError(t, err)
	var total int
	for {
		_, err = stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		total++
	}
	require.Equal(t, activesetSize, total)
}
