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
	"github.com/spacemeshos/ed25519"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/mocks"
	"github.com/spacemeshos/go-spacemesh/cmd"
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
	"github.com/spacemeshos/go-spacemesh/txs"
)

const (
	labelsPerUnit    = 2048
	bitsPerLabel     = 8
	numUnits         = 2
	genTimeUnix      = 1000000
	layerDurationSec = 10
	layerAvgSize     = 10
	txsPerProposal   = 99
	layersPerEpoch   = uint32(5)

	atxPerLayer    = 2
	blkPerLayer    = 3
	accountBalance = 8675301
	accountCounter = 0
	rewardAmount   = 5551234
	receiptIndex   = 42
)

var (
	txReturnLayer         = types.NewLayerID(1)
	layerFirst            = types.NewLayerID(0)
	layerVerified         = types.NewLayerID(8)
	layerLatest           = types.NewLayerID(10)
	layerCurrent          = types.NewLayerID(12)
	postGenesisEpochLayer = types.NewLayerID(22)
	genesisID             = types.Hash20{}

	networkMock = NetworkMock{}
	genTime     = GenesisTimeMock{time.Unix(genTimeUnix, 0)}
	addr1       types.Address
	addr2       types.Address
	prevAtxID   = types.ATXID(types.HexToHash32("44444"))
	chlng       = types.HexToHash32("55555")
	poetRef     = []byte("66666")
	nipost      = newNIPostWithChallenge(&chlng, poetRef)
	challenge   = newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	globalAtx   *types.VerifiedActivationTx
	globalAtx2  *types.VerifiedActivationTx
	signer      *signing.EdSigner
	signer1     *signing.EdSigner
	signer2     *signing.EdSigner
	globalTx    *types.Transaction
	globalTx2   *types.Transaction
	ballot1     = genLayerBallot(types.NewLayerID(11))
	block1      = genLayerBlock(types.NewLayerID(11), nil)
	block2      = genLayerBlock(types.NewLayerID(11), nil)
	block3      = genLayerBlock(types.NewLayerID(11), nil)
	meshAPI     = &MeshAPIMock{}
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
	b.Signature = signer.Sign(b.SignedBytes())
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

func dialGrpc(ctx context.Context, tb testing.TB, cfg config.Config) *grpc.ClientConn {
	tb.Helper()
	conn, err := grpc.DialContext(ctx,
		"localhost:"+strconv.Itoa(cfg.GrpcServerPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(tb, err)
	tb.Cleanup(func() { require.NoError(tb, conn.Close()) })
	return conn
}

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	// run on a random port
	cfg.GrpcServerPort = 1024 + rand.Intn(9999)

	var err error
	signer, err = signing.NewEdSigner()
	if err != nil {
		log.Println("failed to create signer:", err)
		os.Exit(1)
	}
	nodeID := signer.NodeID()
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

	atx := types.NewActivationTx(challenge, &nodeID, addr1, nipost, numUnits, nil, nil)
	if err := activation.SignAndFinalizeAtx(signer, atx); err != nil {
		log.Println("failed to sign atx:", err)
		os.Exit(1)
	}
	globalAtx, err = atx.Verify(0, 1)
	if err != nil {
		log.Println("failed to verify atx:", err)
		os.Exit(1)
	}

	atx2 := types.NewActivationTx(challenge, &nodeID, addr2, nipost, numUnits, nil, nil)
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
	ballot1.EpochData = &types.EpochData{ActiveSet: []types.ATXID{globalAtx.ID(), globalAtx2.ID()}}

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
	return &types.NIPost{
		Challenge: challenge,
		Post: &types.Post{
			Nonce:   0,
			Indices: []byte(nil),
		},
		PostMetadata: &types.PostMetadata{
			Challenge:     poetRef,
			LabelsPerUnit: labelsPerUnit,
			BitsPerLabel:  bitsPerLabel,
		},
	}
}

type NetworkMock struct{}

func (s *NetworkMock) PeerCount() uint64 {
	return 0
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
	if tid.After(genTime.GetCurrentLayer()) {
		return nil, errors.New("requested layer later than current layer")
	} else if tid.After(m.LatestLayer()) {
		return nil, errors.New("haven't received that layer yet")
	}

	ballots := []*types.Ballot{ballot1}
	blocks := []*types.Block{block1, block2, block3}
	return types.NewExistingLayer(tid,
		types.CalcBlocksHash32(types.ToBlockIDs(blocks), nil),
		ballots, blocks), nil
}

func (m *MeshAPIMock) GetATXs(context.Context, []types.ATXID) (map[types.ATXID]*types.VerifiedActivationTx, []types.ATXID) {
	atxs := map[types.ATXID]*types.VerifiedActivationTx{
		globalAtx.ID():  globalAtx,
		globalAtx2.ID(): globalAtx2,
	}
	return atxs, nil
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

func (t *ConStateAPIMock) Put(id types.TransactionID, tx *types.Transaction) {
	t.poolByTxId[id] = tx
	t.poolByAddress[tx.Principal] = id
	events.ReportNewTx(types.LayerID{}, tx)
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

func (t *ConStateAPIMock) GetLayerApplied(txID types.TransactionID) (types.LayerID, error) {
	return *t.layerApplied[txID], nil
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

func newChallenge(sequence uint64, prevAtxID, posAtxID types.ATXID, pubLayerID types.LayerID) types.NIPostChallenge {
	return types.NIPostChallenge{
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PubLayerID:     pubLayerID,
		PositioningATX: posAtxID,
	}
}

// PostAPIMock is a mock for Post API.
// TODO(mafa): replace this mock with the generated mock.
type PostAPIMock struct{}

func (*PostAPIMock) Status() *activation.PostSetupStatus {
	return &activation.PostSetupStatus{}
}

func (p *PostAPIMock) StatusChan() <-chan *activation.PostSetupStatus {
	ch := make(chan *activation.PostSetupStatus, 1)
	ch <- p.Status()
	close(ch)

	return ch
}

func (p *PostAPIMock) ComputeProviders() []activation.PostSetupComputeProvider {
	return nil
}

func (p *PostAPIMock) Benchmark(activation.PostSetupComputeProvider) (int, error) {
	return 0, nil
}

func (p *PostAPIMock) StartSession(opts activation.PostSetupOpts, commitmentAtx types.ATXID) (chan struct{}, error) {
	return nil, nil
}

func (p *PostAPIMock) StopSession(deleteFiles bool) error {
	return nil
}

func (p *PostAPIMock) GenerateProof(challenge []byte, commitmentAtx types.ATXID) (*types.Post, *types.PostMetadata, error) {
	return &types.Post{}, &types.PostMetadata{}, nil
}

func (p *PostAPIMock) LastError() error {
	return nil
}

func (p *PostAPIMock) LastOpts() *activation.PostSetupOpts {
	return &activation.PostSetupOpts{}
}

func (p *PostAPIMock) Config() activation.PostConfig {
	return activation.PostConfig{}
}

// SmeshingAPIMock is a mock for Smeshing API.
type SmeshingAPIMock struct{}

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

type GenesisTimeMock struct {
	t time.Time
}

func (t GenesisTimeMock) GetCurrentLayer() types.LayerID {
	return types.LayerID(layerCurrent)
}

func (t GenesisTimeMock) GetGenesisTime() time.Time {
	return t.t
}

func marshalProto(t *testing.T, msg proto.Message) string {
	var buf bytes.Buffer
	var m jsonpb.Marshaler
	require.NoError(t, m.Marshal(&buf, msg))
	return buf.String()
}

var cfg = config.DefaultTestConfig()

type SyncerMock struct {
	startCalled bool
	isSynced    bool
}

func (s *SyncerMock) IsSynced(context.Context) bool { return s.isSynced }
func (s *SyncerMock) Start(context.Context)         { s.startCalled = true }

type ActivationAPIMock struct {
	UpdatePoETErr error
}

func (a *ActivationAPIMock) UpdatePoETServers(context.Context, []string) error {
	return a.UpdatePoETErr
}

func launchServer(tb testing.TB, services ...ServiceAPI) func() {
	grpcService := NewServerWithInterface(cfg.GrpcServerPort, "localhost")
	jsonService := NewJSONHTTPServer(cfg.JSONServerPort)

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
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", cfg.JSONServerPort, endpoint)
	resp, err := http.Post(url, "application/json", strings.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return string(buf), resp.StatusCode
}

func getUnboundedPort(optionalPort int) (int, error) {
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
	port1, err := getUnboundedPort(0)
	require.NoError(t, err, "Should be able to establish a connection on a port")

	port2, err := getUnboundedPort(0)
	require.NoError(t, err, "Should be able to establish a connection on a port")

	grpcService := NewServerWithInterface(port1, "localhost")
	jsonService := NewJSONHTTPServer(port2)

	require.Equal(t, port2, jsonService.port, "Expected same port")
	require.Equal(t, port1, grpcService.Port, "Expected same port")
}

func TestNodeService(t *testing.T) {
	logtest.SetupGlobal(t)
	syncer := SyncerMock{}
	atxapi := &ActivationAPIMock{}
	grpcService := NewNodeService(&networkMock, meshAPI, &genTime, &syncer, atxapi)
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
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
			require.EqualError(t, err, "rpc error: code = InvalidArgument desc = Must include `Msg`")
			statusCode := status.Code(err)
			require.Equal(t, codes.InvalidArgument, statusCode)

			_, err = c.Echo(context.Background(), &pb.EchoRequest{})
			require.EqualError(t, err, "rpc error: code = InvalidArgument desc = Must include `Msg`")
			statusCode = status.Code(err)
			require.Equal(t, codes.InvalidArgument, statusCode)
		}},
		{"Version", func(t *testing.T) {
			logtest.SetupGlobal(t)
			// must set this manually as it's set up in main() when running
			version := "abc123"
			cmd.Version = version
			res, err := c.Version(context.Background(), &empty.Empty{})
			require.NoError(t, err)
			require.Equal(t, version, res.VersionString.Value)
		}},
		{"Build", func(t *testing.T) {
			logtest.SetupGlobal(t)
			// must set this manually as it's set up in main() when running
			build := "abc123"
			cmd.Commit = build
			res, err := c.Build(context.Background(), &empty.Empty{})
			require.NoError(t, err)
			require.Equal(t, build, res.BuildString.Value)
		}},
		{"Status", func(t *testing.T) {
			logtest.SetupGlobal(t)
			// First do a mock checking during a genesis layer
			// During genesis all layers should be set to current layer
			oldCurLayer := layerCurrent
			layerCurrent = types.NewLayerID(layersPerEpoch) // end of first epoch
			req := &pb.StatusRequest{}
			res, err := c.Status(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, uint64(0), res.Status.ConnectedPeers)
			require.Equal(t, false, res.Status.IsSynced)
			require.Equal(t, layerLatest.Uint32(), res.Status.SyncedLayer.Number)
			require.Equal(t, layerCurrent.Uint32(), res.Status.TopLayer.Number)
			require.Equal(t, layerLatest.Uint32(), res.Status.VerifiedLayer.Number)

			// Now do a mock check post-genesis
			layerCurrent = oldCurLayer
			res, err = c.Status(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, uint64(0), res.Status.ConnectedPeers)
			require.Equal(t, false, res.Status.IsSynced)
			require.Equal(t, layerLatest.Uint32(), res.Status.SyncedLayer.Number)
			require.Equal(t, layerCurrent.Uint32(), res.Status.TopLayer.Number)
			require.Equal(t, layerVerified.Uint32(), res.Status.VerifiedLayer.Number)
		}},
		{"SyncStart", func(t *testing.T) {
			logtest.SetupGlobal(t)
			require.Equal(t, false, syncer.startCalled, "Start() not yet called on syncer")
			req := &pb.SyncStartRequest{}
			res, err := c.SyncStart(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, int32(code.Code_OK), res.Status.Code)
			require.Equal(t, true, syncer.startCalled, "Start() was called on syncer")
		}},
		{"Shutdown", func(t *testing.T) {
			logtest.SetupGlobal(t)
			req := &pb.ShutdownRequest{}
			res, err := c.Shutdown(context.Background(), req)
			require.Nil(t, res)
			require.ErrorIs(t, err, status.Errorf(codes.Unimplemented, "UNIMPLEMENTED"))
		}},
		{"UpdatePoetServer", func(t *testing.T) {
			logtest.SetupGlobal(t)
			atxapi.UpdatePoETErr = nil
			res, err := c.UpdatePoetServers(context.Background(), &pb.UpdatePoetServersRequest{Urls: []string{"test"}})
			require.NoError(t, err)
			require.EqualValues(t, res.Status.Code, code.Code_OK)
		}},
		{"UpdatePoetServerUnavailable", func(t *testing.T) {
			logtest.SetupGlobal(t)
			atxapi.UpdatePoETErr = activation.ErrPoetServiceUnstable
			urls := []string{"test"}
			res, err := c.UpdatePoetServers(context.Background(), &pb.UpdatePoetServersRequest{Urls: urls})
			require.Nil(t, res)
			require.ErrorIs(t, err, status.Errorf(codes.Unavailable, "can't reach poet service (%v). retry later", atxapi.UpdatePoETErr))
		}},
		// NOTE: ErrorStream and StatusStream have comprehensive, E2E tests in cmd/node/node_test.go.
	}

	for _, tc := range testCases {
		t.Run(tc.name, tc.run)
	}
}

func TestGlobalStateService(t *testing.T) {
	logtest.SetupGlobal(t)
	svc := NewGlobalStateService(meshAPI, conStateAPI)
	shutDown := launchServer(t, svc)
	defer shutDown()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
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
	svc := NewSmesherService(&PostAPIMock{}, &SmeshingAPIMock{}, 10*time.Millisecond)
	shutDown := launchServer(t, svc)
	defer shutDown()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
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
		_, err := c.PostSetupComputeProviders(context.Background(), &pb.PostSetupComputeProvidersRequest{Benchmark: false})
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
}

func TestMeshService(t *testing.T) {
	logtest.SetupGlobal(t)
	grpcService := NewMeshService(meshAPI, conStateAPI, &genTime, layersPerEpoch, types.Hash20{}, layerDurationSec, layerAvgSize, txsPerProposal)
	shutDown := launchServer(t, grpcService)
	t.Cleanup(func() { shutDown() })

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
			logtest.SetupGlobal(t)
			response, err := c.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(genTime.GetGenesisTime().Unix()), response.Unixtime.Value)
		}},
		{"CurrentLayer", func(t *testing.T) {
			logtest.SetupGlobal(t)
			response, err := c.CurrentLayer(context.Background(), &pb.CurrentLayerRequest{})
			require.NoError(t, err)
			require.Equal(t, uint32(12), response.Layernum.Number)
		}},
		{"CurrentEpoch", func(t *testing.T) {
			logtest.SetupGlobal(t)
			response, err := c.CurrentEpoch(context.Background(), &pb.CurrentEpochRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(2), response.Epochnum.Value)
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
			require.Equal(t, uint64(layerDurationSec), response.Duration.Value)
		}},
		{"MaxTransactionsPerSecond", func(t *testing.T) {
			logtest.SetupGlobal(t)
			response, err := c.MaxTransactionsPerSecond(context.Background(), &pb.MaxTransactionsPerSecondRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(layerAvgSize*txsPerProposal/layerDurationSec), response.MaxTxsPerSecond.Value)
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
	syncer := &SyncerMock{}
	ctrl := gomock.NewController(t)
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	grpcService := NewTransactionService(sql.InMemory(), publisher, meshAPI, conStateAPI, syncer)
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewTransactionServiceClient(conn)

	serializedTx, err := codec.Encode(globalTx)
	req.NoError(err, "error serializing tx")

	// This time, we expect an error, since isSynced is false (by default)
	// The node should not allow tx submission when not synced
	res, err := c.SubmitTransaction(ctx, &pb.SubmitTransactionRequest{Transaction: serializedTx})
	req.EqualError(err, "rpc error: code = FailedPrecondition desc = Cannot submit transaction, node is not in sync yet, try again later")
	req.Nil(res)

	syncer.isSynced = true

	// This time, we expect no error, since isSynced is now true
	_, err = c.SubmitTransaction(ctx, &pb.SubmitTransactionRequest{Transaction: serializedTx})
	req.NoError(err)
	// TODO: randomly got an error here, should investigate. Added specific error check above, as this error should have
	//  happened there first.
	//  Received unexpected error: "rpc error: code = Unimplemented desc = unknown service spacemesh.v1.TransactionService"
}

func TestTransactionService_SubmitNoConcurrency(t *testing.T) {
	logtest.SetupGlobal(t)

	ctrl := gomock.NewController(t)
	publisher := pubsubmocks.NewMockPublisher(ctrl)

	expected := 20
	n := 0
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, _ string, msg []byte) error {
		n++
		return nil
	})
	grpcService := NewTransactionService(sql.InMemory(), publisher, meshAPI, conStateAPI, &SyncerMock{isSynced: true})
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewTransactionServiceClient(conn)
	for i := 0; i < expected; i++ {
		res, err := c.SubmitTransaction(ctx, &pb.SubmitTransactionRequest{
			Transaction: globalTx.Raw,
		})
		require.NoError(t, err)
		require.Equal(t, int32(code.Code_OK), res.Status.Code)
		require.Equal(t, globalTx.ID.Bytes(), res.Txstate.Id.Id)
		require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res.Txstate.State)
	}
	require.Equal(t, expected, n)
}

func TestTransactionService(t *testing.T) {
	logtest.SetupGlobal(t)

	ctrl := gomock.NewController(t)
	publisher := pubsubmocks.NewMockPublisher(ctrl)

	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	grpcService := NewTransactionService(sql.InMemory(), publisher, meshAPI, conStateAPI, &SyncerMock{isSynced: true})
	shutDown := launchServer(t, grpcService)
	defer shutDown()

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

			events.ReportNewTx(types.LayerID{}, globalTx)
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

			events.ReportNewTx(types.LayerID{}, globalTx)

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
					conStateAPI.Put(globalTx.ID, globalTx)
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

			events.ReportNewTx(types.LayerID{}, globalTx)

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
				events.ReportNewTx(types.LayerID{}, globalTx)
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
			if a.Layer.Number != globalAtx.PubLayerID.Uint32() {
				continue
			}
			if !bytes.Equal(a.Id.Id, globalAtx.ID().Bytes()) {
				continue
			}
			if !bytes.Equal(a.SmesherId.Id, globalAtx.NodeID().Bytes()) {
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

	grpcService := NewMeshService(meshAPI, conStateAPI, &genTime, layersPerEpoch, types.Hash20{}, layerDurationSec, layerAvgSize, txsPerProposal)
	shutDown := launchServer(t, grpcService)
	defer shutDown()

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
	events.ReportNewTx(types.LayerID{}, globalTx)
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
	events.ReportNewTx(types.LayerID{}, globalTx2)
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

	svc := NewGlobalStateService(meshAPI, conStateAPI)
	shutDown := launchServer(t, svc)
	t.Cleanup(shutDown)

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
	require.Equal(t, status.Convert(err).Code(), codes.DeadlineExceeded)
}

func TestGlobalStateStream_comprehensive(t *testing.T) {
	logtest.SetupGlobal(t)
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	svc := NewGlobalStateService(meshAPI, conStateAPI)
	shutDown := launchServer(t, svc)
	defer shutDown()

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
	layer, err := meshAPI.GetLayer(layerFirst)
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

	grpcService := NewMeshService(meshAPI, conStateAPI, &genTime, layersPerEpoch, types.Hash20{}, layerDurationSec, layerAvgSize, txsPerProposal)
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)

	// set up the grpc listener stream
	c := pb.NewMeshServiceClient(conn)
	stream, err := c.LayerStream(ctx, &pb.LayerStreamRequest{})
	require.NoError(t, err, "stream request returned unexpected error")
	// Give the server-side time to subscribe to events
	time.Sleep(time.Millisecond * 50)

	layer, err := meshAPI.GetLayer(layerFirst)
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
	require.Equal(t, globalAtx.PubLayerID.Uint32(), x.Activation.Layer.Number)
	require.Equal(t, globalAtx.NodeID().Bytes(), x.Activation.SmesherId.Id)
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
	cfg.GrpcServerPort = 9192
	svc1 := NewNodeService(&networkMock, meshAPI, &genTime, &SyncerMock{}, &ActivationAPIMock{})
	svc2 := NewMeshService(meshAPI, conStateAPI, &genTime, layersPerEpoch, types.Hash20{}, layerDurationSec, layerAvgSize, txsPerProposal)
	shutDown := launchServer(t, svc1, svc2)
	defer shutDown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)

	c1 := pb.NewNodeServiceClient(conn)
	c2 := pb.NewMeshServiceClient(conn)

	// call endpoints and validate results
	const message = "Hello World"
	res1, err1 := c1.Echo(ctx, &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message},
	})
	require.NoError(t, err1)
	require.Equal(t, message, res1.Msg.Value)
	res2, err2 := c2.GenesisTime(ctx, &pb.GenesisTimeRequest{})
	require.NoError(t, err2)
	require.Equal(t, uint64(genTime.GetGenesisTime().Unix()), res2.Unixtime.Value)

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
	cfg.StartNodeService = false
	cfg.StartMeshService = false
	shutDown := launchServer(t)
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", cfg.JSONServerPort, "v1/node/echo")
	_, err := http.Post(url, "application/json", strings.NewReader(payload))
	require.Error(t, err)
	shutDown()

	// enable services and try again
	svc1 := NewNodeService(&networkMock, meshAPI, &genTime, &SyncerMock{}, &ActivationAPIMock{})
	svc2 := NewMeshService(meshAPI, conStateAPI, &genTime, layersPerEpoch, types.Hash20{}, layerDurationSec, layerAvgSize, txsPerProposal)
	cfg.StartNodeService = true
	cfg.StartMeshService = true
	shutDown = launchServer(t, svc1, svc2)
	defer shutDown()
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
	require.Equal(t, uint64(genTime.GetGenesisTime().Unix()), msg2.Unixtime.Value)
}

func TestDebugService(t *testing.T) {
	logtest.SetupGlobal(t)
	ctrl := gomock.NewController(t)
	identity := mocks.NewMockNetworkIdentity(ctrl)
	svc := NewDebugService(conStateAPI, identity)
	shutDown := launchServer(t, svc)
	defer shutDown()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewDebugServiceClient(conn)

	t.Run("Accounts", func(t *testing.T) {
		res, err := c.Accounts(context.Background(), &empty.Empty{})
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
	t.Run("networkID", func(t *testing.T) {
		id := p2p.Peer("test")
		identity.EXPECT().ID().Return(id)

		response, err := c.NetworkInfo(context.Background(), &empty.Empty{})
		require.NoError(t, err)
		require.NotNil(t, response)
		require.Equal(t, id.String(), response.Id)
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

func TestGatewayService(t *testing.T) {
	logtest.SetupGlobal(t)
	ctrl := gomock.NewController(t)
	verifier := mocks.NewMockChallengeVerifier(ctrl)
	verifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(&activation.ChallengeVerificationResult{}, nil)

	svc := NewGatewayService(verifier)
	shutDown := launchServer(t, svc)
	defer shutDown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialGrpc(ctx, t, cfg)
	c := pb.NewGatewayServiceClient(conn)

	req := &pb.VerifyChallengeRequest{}
	_, err := c.VerifyChallenge(ctx, req)
	s, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.OK, s.Code())
}

func TestEventsReceived(t *testing.T) {
	logtest.SetupGlobal(t)
	events.CloseEventReporter()
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	ctrl := gomock.NewController(t)
	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, _ string, msg []byte) error {
		return nil
	})

	txService := NewTransactionService(sql.InMemory(), publisher, meshAPI, conStateAPI, &SyncerMock{isSynced: true})
	gsService := NewGlobalStateService(meshAPI, conStateAPI)
	shutDown := launchServer(t, txService, gsService)
	defer shutDown()

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
				pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_TRANSACTION_RECEIPT),
		},
	}

	receiverReq := &pb.AccountDataStreamRequest{
		Filter: &pb.AccountDataFilter{
			AccountId: &pb.AccountId{Address: addr2.String()},
			AccountDataFlags: uint32(
				pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD |
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
	rewards := []types.AnyReward{{Coinbase: addr2, Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}}}
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

func TestVMAccountUpdates(t *testing.T) {
	logtest.SetupGlobal(t)
	events.CloseEventReporter()
	events.InitializeReporter()

	// in memory database doesn't allow reads while writer locked db
	db, err := sql.Open("file:" + filepath.Join(t.TempDir(), "test.sql"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	svm := vm.New(db, vm.WithLogger(logtest.New(t)))
	t.Cleanup(launchServer(t, NewGlobalStateService(nil, txs.NewConservativeState(svm, db))))

	keys := make([]ed25519.PrivateKey, 10)
	accounts := make([]types.Account, len(keys))
	const initial = 100_000_000
	for i := range keys {
		pub, pk, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		keys[i] = pk
		accounts[i] = types.Account{
			Address: wallet.Address(pub),
			Balance: initial,
		}
	}
	require.NoError(t, svm.ApplyGenesis(accounts))
	spawns := []types.Transaction{}
	for _, pk := range keys {
		spawns = append(spawns, types.Transaction{
			RawTx: types.NewRawTx(wallet.SelfSpawn(pk, 0)),
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
	for _, pk := range keys {
		spends = append(spends, types.Transaction{
			RawTx: types.NewRawTx(wallet.Spend(
				pk, types.Address{1}, amount, 1,
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
