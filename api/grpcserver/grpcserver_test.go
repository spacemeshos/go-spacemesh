package grpcserver

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/signing"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"io/ioutil"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/require"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

const (
	miningStatus          = 123
	remainingBytes        = 321
	defaultGasLimit       = 10
	defaultFee            = 1
	genTimeUnix           = 1000000
	layerDurationSec      = 10
	layerAvgSize          = 10
	txsPerBlock           = 99
	TxReturnLayer         = 1
	layersPerEpoch        = 5
	networkID             = 120
	postGenesisEpochLayer = 22
	atxPerLayer           = 2
	blkPerLayer           = 3
	accountBalance        = 8675301
	accountCounter        = 42
	layerFirst            = 0
	layerVerified         = 8
	layerLatest           = 10
	layerCurrent          = 12
	rewardAmount          = 5551234
)

var (
	networkMock = NetworkMock{}
	genTime     = GenesisTimeMock{time.Unix(genTimeUnix, 0)}
	txMempool   = miner.NewTxMemPool()
	addr1       = types.HexToAddress("33333")
	addr2       = types.HexToAddress("44444")
	pub, _, _   = ed25519.GenerateKey(nil)
	nodeID      = types.NodeID{Key: util.Bytes2Hex(pub), VRFPublicKey: []byte("22222")}
	prevAtxID   = types.ATXID(types.HexToHash32("44444"))
	chlng       = types.HexToHash32("55555")
	poetRef     = []byte("66666")
	npst        = activation.NewNIPSTWithChallenge(&chlng, poetRef)
	challenge   = newChallenge(nodeID, 1, prevAtxID, prevAtxID, postGenesisEpochLayer)
	globalAtx   = newAtx(challenge, 5, defaultView, npst, addr1)
	globalAtx2  = newAtx(challenge, 5, defaultView, npst, addr2)
	globalTx    = NewTx(1, addr1, signing.NewEdSigner())
	globalTx2   = NewTx(1, addr2, signing.NewEdSigner())
	block1      = types.NewExistingBlock(0, []byte("11111"))
	block2      = types.NewExistingBlock(0, []byte("22222"))
	block3      = types.NewExistingBlock(0, []byte("33333"))
	defaultView = []types.BlockID{block1.ID(), block2.ID(), block3.ID()}
	txAPI       = &TxAPIMock{
		returnTx:     make(map[types.TransactionID]*types.Transaction),
		layerApplied: make(map[types.TransactionID]*types.LayerID),
	}
	stateRoot = types.HexToHash32("11111")
)

func init() {
	// These create circular dependencies so they have to be initialized
	// after the global vars
	block1.TxIDs = []types.TransactionID{globalTx.ID(), globalTx2.ID()}
	block1.ATXIDs = []types.ATXID{globalAtx.ID(), globalAtx2.ID()}
	txAPI.returnTx[globalTx.ID()] = globalTx
	txAPI.returnTx[globalTx2.ID()] = globalTx2
}

// Better a small code duplication than a small dependency

type NodeAPIMock struct {
	balances map[types.Address]*big.Int
	nonces   map[types.Address]uint64
}

type NetworkMock struct {
	lock         sync.Mutex
	broadCastErr bool
	broadcasted  []byte
}

func (s *NetworkMock) SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey) {
	return make(chan p2pcrypto.PublicKey), make(chan p2pcrypto.PublicKey)
}

func (s *NetworkMock) Broadcast(_ string, payload []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.broadCastErr {
		return errors.New("error during broadcast")
	}
	s.broadcasted = payload
	return nil
}

func (s *NetworkMock) GetBroadcast() []byte {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.broadcasted
}

func (s *NetworkMock) SetErr(err bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.broadCastErr = err
}

func (s *NetworkMock) GetErr() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.broadCastErr
}

func NewNodeAPIMock() NodeAPIMock {
	return NodeAPIMock{
		balances: make(map[types.Address]*big.Int),
		nonces:   make(map[types.Address]uint64),
	}
}

func (n NodeAPIMock) GetBalance(address types.Address) uint64 {
	return n.balances[address].Uint64()
}

func (n NodeAPIMock) GetNonce(address types.Address) uint64 {
	return n.nonces[address]
}

func (n NodeAPIMock) Exist(address types.Address) bool {
	_, ok := n.nonces[address]
	return ok
}

type TxAPIMock struct {
	mockOrigin   types.Address
	returnTx     map[types.TransactionID]*types.Transaction
	layerApplied map[types.TransactionID]*types.LayerID
	err          error
}

func (t *TxAPIMock) GetStateRoot() types.Hash32 {
	return stateRoot
}

func (t *TxAPIMock) ValidateNonceAndBalance(*types.Transaction) error {
	return t.err
}

func (t *TxAPIMock) GetProjection(_ types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error) {
	return prevNonce, prevBalance, nil
}

// latest layer received
func (t *TxAPIMock) LatestLayer() types.LayerID {
	return layerLatest
}

// latest layer approved/confirmed/applied to state
// The real logic here is a bit more complicated, as it depends whether the node
// is syncing or not. If it's not syncing, layers are applied to state as they're
// verified by Hare. If it's syncing, Hare is not run, and they are applied to
// state as they're confirmed by Tortoise and it advances pbase. This is all in
// flux right now so keep this simple for the purposes of testing.
func (t *TxAPIMock) LatestLayerInState() types.LayerID {
	return layerVerified
}

func (t *TxAPIMock) GetLayerApplied(txID types.TransactionID) *types.LayerID {
	return t.layerApplied[txID]
}

func (t *TxAPIMock) GetTransaction(id types.TransactionID) (*types.Transaction, error) {
	return t.returnTx[id], nil
}

func (t *TxAPIMock) GetRewards(types.Address) (rewards []types.Reward, err error) {
	return []types.Reward{
		{
			Layer:               layerFirst,
			TotalReward:         rewardAmount,
			LayerRewardEstimate: rewardAmount,
		},
	}, nil
}

func (t *TxAPIMock) GetTransactionsByDestination(l types.LayerID, account types.Address) (txs []types.TransactionID) {
	if l != TxReturnLayer {
		return nil
	}
	for _, tx := range t.returnTx {
		if tx.Recipient.String() == account.String() {
			txs = append(txs, tx.ID())
		}
	}
	return
}

func (t *TxAPIMock) GetTransactionsByOrigin(l types.LayerID, account types.Address) (txs []types.TransactionID) {
	if l != TxReturnLayer {
		return nil
	}
	for _, tx := range t.returnTx {
		if tx.Origin().String() == account.String() {
			txs = append(txs, tx.ID())
		}
	}
	return
}

func (t *TxAPIMock) setMockOrigin(orig types.Address) {
	t.mockOrigin = orig
}

func (t *TxAPIMock) AddressExists(types.Address) bool {
	return true
}

func (t *TxAPIMock) GetLayer(tid types.LayerID) (*types.Layer, error) {
	if tid > genTime.GetCurrentLayer() {
		return nil, errors.New("requested layer later than current layer")
	} else if tid > t.LatestLayer() {
		return nil, errors.New("haven't received that layer yet")
	}

	blocks := []*types.Block{block1, block2, block3}
	return types.NewExistingLayer(tid, blocks), nil
}

func (t *TxAPIMock) GetATXs([]types.ATXID) (map[types.ATXID]*types.ActivationTx, []types.ATXID) {
	atxs := map[types.ATXID]*types.ActivationTx{
		globalAtx.ID():  globalAtx,
		globalAtx2.ID(): globalAtx2,
	}
	return atxs, nil
}

func (t *TxAPIMock) GetTransactions(txids []types.TransactionID) (txs []*types.Transaction, missing map[types.TransactionID]struct{}) {
	for _, txid := range txids {
		for _, tx := range t.returnTx {
			if tx.ID() == txid {
				txs = append(txs, tx)
			}
		}
	}
	return
}

func (t *TxAPIMock) ProcessedLayer() types.LayerID {
	return 8
}

func (t *TxAPIMock) GetLayerStateRoot(layer types.LayerID) (types.Hash32, error) {
	return stateRoot, nil
}

func (t *TxAPIMock) GetBalance(addr types.Address) uint64 {
	return uint64(accountBalance)
}

func (t *TxAPIMock) GetNonce(addr types.Address) uint64 {
	return uint64(accountCounter)
}

func (t *TxAPIMock) ProcessedLayer() types.LayerID {
	return types.LayerID(layerVerified)
}

func NewTx(nonce uint64, recipient types.Address, signer *signing.EdSigner) *types.Transaction {
	tx, err := types.NewSignedTx(nonce, recipient, 1, defaultGasLimit, defaultFee, signer)
	if err != nil {
		log.Error("error creating new signed tx: ", err)
		return nil
	}
	return tx
}

func newChallenge(nodeID types.NodeID, sequence uint64, prevAtxID, posAtxID types.ATXID, pubLayerID types.LayerID) types.NIPSTChallenge {
	challenge := types.NIPSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PubLayerID:     pubLayerID,
		PositioningATX: posAtxID,
	}
	return challenge
}

func newAtx(challenge types.NIPSTChallenge, ActiveSetSize uint32, View []types.BlockID, nipst *types.NIPST, coinbase types.Address) *types.ActivationTx {
	activationTx := &types.ActivationTx{
		InnerActivationTx: &types.InnerActivationTx{
			ActivationTxHeader: &types.ActivationTxHeader{
				NIPSTChallenge: challenge,
				Coinbase:       coinbase,
				ActiveSetSize:  ActiveSetSize,
			},
			Nipst: nipst,
			View:  View,
		},
	}
	activationTx.CalcAndSetID()
	return activationTx
}

// MiningAPIMock is a mock for mining API
type MiningAPIMock struct{}

func (*MiningAPIMock) MiningStats() (int, uint64, string, string) {
	return miningStatus, remainingBytes, "123456", "/tmp"
}

func (*MiningAPIMock) StartPost(types.Address, string, uint64) error {
	return nil
}

func (*MiningAPIMock) SetCoinbaseAccount(types.Address) {}

type OracleMock struct{}

func (*OracleMock) GetEligibleLayers() []types.LayerID {
	return []types.LayerID{1, 2, 3, 4}
}

type GenesisTimeMock struct {
	t time.Time
}

func (t GenesisTimeMock) GetCurrentLayer() types.LayerID {
	return layerCurrent
}

func (t GenesisTimeMock) GetGenesisTime() time.Time {
	return t.t
}

type PostMock struct {
}

func (PostMock) Reset() error {
	return nil
}

func marshalProto(t *testing.T, msg proto.Message) string {
	var buf bytes.Buffer
	var m jsonpb.Marshaler
	require.NoError(t, m.Marshal(&buf, msg))
	return buf.String()
}

var cfg = config.DefaultConfig()

type SyncerMock struct {
	startCalled bool
}

func (SyncerMock) IsSynced() bool { return false }
func (s *SyncerMock) Start()      { s.startCalled = true }

func launchServer(t *testing.T, services ...ServiceAPI) func() {
	networkMock.Broadcast("", []byte{0x00})
	grpcService := NewServerWithInterface(cfg.NewGrpcServerPort, "localhost")
	jsonService := NewJSONHTTPServer(cfg.NewJSONServerPort, cfg.NewGrpcServerPort)

	// attach services
	for _, svc := range services {
		svc.RegisterService(grpcService)
	}

	// start gRPC and json servers
	grpcService.Start()
	jsonService.StartService(cfg.StartNodeService, cfg.StartMeshService, cfg.StartGlobalStateService,
		cfg.StartSmesherService, cfg.StartTransactionService)
	time.Sleep(3 * time.Second) // wait for server to be ready (critical on Travis)

	return func() {
		require.NoError(t, jsonService.Close())
		grpcService.Close()
	}
}

func callEndpoint(t *testing.T, endpoint, payload string) (string, int) {
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", cfg.NewJSONServerPort, endpoint)
	t.Log("sending POST request to", url)
	resp, err := http.Post(url, "application/json", strings.NewReader(payload))
	t.Log("got response", resp)
	require.NoError(t, err)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	buf, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return string(buf), resp.StatusCode
}

func TestNewServersConfig(t *testing.T) {
	port1, err := node.GetUnboundedPort()
	port2, err := node.GetUnboundedPort()
	require.NoError(t, err, "Should be able to establish a connection on a port")

	grpcService := NewServerWithInterface(port1, "localhost")
	jsonService := NewJSONHTTPServer(port2, port1)

	require.Equal(t, port2, jsonService.Port, "Expected same port")
	require.Equal(t, port1, jsonService.GrpcPort, "Expected same port")
	require.Equal(t, port1, grpcService.Port, "Expected same port")
}

func TestNodeService(t *testing.T) {
	syncer := SyncerMock{}
	grpcService := NewNodeService(&networkMock, txAPI, &genTime, &syncer)
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.NewGrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c := pb.NewNodeServiceClient(conn)

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"Echo", func(t *testing.T) {
			const message = "Hello World"
			res, err := c.Echo(context.Background(), &pb.EchoRequest{
				Msg: &pb.SimpleString{Value: message}})
			require.NoError(t, err)
			require.Equal(t, message, res.Msg.Value)

			// now try sending bad payloads
			_, err = c.Echo(context.Background(), &pb.EchoRequest{Msg: nil})
			require.EqualError(t, err, "rpc error: code = InvalidArgument desc = Must include `Msg`")
			code := status.Code(err)
			require.Equal(t, codes.InvalidArgument, code)

			_, err = c.Echo(context.Background(), &pb.EchoRequest{})
			require.EqualError(t, err, "rpc error: code = InvalidArgument desc = Must include `Msg`")
			code = status.Code(err)
			require.Equal(t, codes.InvalidArgument, code)
		}},
		{"Version", func(t *testing.T) {
			// must set this manually as it's set up in main() when running
			version := "abc123"
			cmd.Version = version
			res, err := c.Version(context.Background(), &empty.Empty{})
			require.NoError(t, err)
			require.Equal(t, version, res.VersionString.Value)
		}},
		{"Build", func(t *testing.T) {
			// must set this manually as it's set up in main() when running
			build := "abc123"
			cmd.Commit = build
			res, err := c.Build(context.Background(), &empty.Empty{})
			require.NoError(t, err)
			require.Equal(t, build, res.BuildString.Value)
		}},
		{"Status", func(t *testing.T) {
			req := &pb.StatusRequest{}
			res, err := c.Status(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, uint64(0), res.Status.ConnectedPeers)
			require.Equal(t, false, res.Status.IsSynced)
			require.Equal(t, uint64(layerLatest), res.Status.SyncedLayer)
			require.Equal(t, uint64(layerCurrent), res.Status.TopLayer)
			require.Equal(t, uint64(layerVerified), res.Status.VerifiedLayer)
		}},
		{"SyncStart", func(t *testing.T) {
			require.Equal(t, false, syncer.startCalled, "Start() not yet called on syncer")
			req := &pb.SyncStartRequest{}
			res, err := c.SyncStart(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, int32(code.Code_OK), res.Status.Code)
			require.Equal(t, true, syncer.startCalled, "Start() was called on syncer")
		}},
		{"Shutdown", func(t *testing.T) {
			called := false
			cmd.Cancel = func() { called = true }
			require.Equal(t, false, called, "cmd.Shutdown() not yet called")
			req := &pb.ShutdownRequest{}
			res, err := c.Shutdown(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, int32(code.Code_OK), res.Status.Code)
			require.Equal(t, true, called, "cmd.Shutdown() was called")
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, tc.run)
	}
}

func TestGlobalStateService(t *testing.T) {
	svc := NewGlobalStateService(&networkMock, txAPI, &genTime, &SyncerMock{})
	shutDown := launchServer(t, svc)
	defer shutDown()

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.NewGrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c := pb.NewGlobalStateServiceClient(conn)

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{"GlobalStateHash", func(t *testing.T) {
			res, err := c.GlobalStateHash(context.Background(), &pb.GlobalStateHashRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(layerVerified), res.Response.LayerNumber)
			require.Equal(t, stateRoot.Bytes(), res.Response.RootHash)
		}},
		{"Account", func(t *testing.T) {
			res, err := c.Account(context.Background(), &pb.AccountRequest{
				AccountId: &pb.AccountId{Address: addr1.Bytes()},
			})
			require.NoError(t, err)
			require.Equal(t, addr1.Bytes(), res.AccountWrapper.AccountId.Address)
			require.Equal(t, uint64(accountBalance), res.AccountWrapper.Balance.Value)
			require.Equal(t, uint64(accountCounter), res.AccountWrapper.Counter)
		}},
		{"AccountDataQuery", func(t *testing.T) {
			// TODO: add tests for missing filter, missing flags, bad offset and max results
			res, err := c.AccountDataQuery(context.Background(), &pb.AccountDataQueryRequest{
				Filter: &pb.AccountDataFilter{
					AccountId: &pb.AccountId{Address: addr1.Bytes()},
					AccountDataFlags: uint32(pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
						pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD),
				},
			})
			require.NoError(t, err)
			require.Equal(t, uint32(2), res.TotalResults)
			require.Equal(t, 2, len(res.AccountItem))
			checkAccountDataQueryItemReward(t, res.AccountItem[0].Item)
			checkAccountDataQueryItemAccount(t, res.AccountItem[1].Item)
		}},
		{"SmesherDataQuery", func(t *testing.T) {
			_, err := c.SmesherDataQuery(context.Background(), &pb.SmesherDataQueryRequest{})
			require.Error(t, err, "expected endpoint to fail")
			require.Contains(t, err.Error(), "this endpoint has not yet been implemented")
		}},
		{name: "AccountDataStream", run: func(t *testing.T) {
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
							AccountId:        &pb.AccountId{Address: addr1.Bytes()},
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
							AccountId: &pb.AccountId{Address: []byte{'A'}},
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

func TestMeshService(t *testing.T) {
	grpcService := NewMeshService(&networkMock, txAPI, txMempool, &genTime, &SyncerMock{}, layersPerEpoch, networkID, layerDurationSec, layerAvgSize, txsPerBlock)
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.NewGrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c := pb.NewMeshServiceClient(conn)

	// Construct an array of test cases to test each endpoint in turn
	testCases := []struct {
		name string
		run  func(*testing.T)
	}{
		{"GenesisTime", func(t *testing.T) {
			response, err := c.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(genTime.GetGenesisTime().Unix()), response.Unixtime.Value)
		}},
		{"CurrentLayer", func(t *testing.T) {
			response, err := c.CurrentLayer(context.Background(), &pb.CurrentLayerRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(12), response.Layernum.Value)
		}},
		{"CurrentEpoch", func(t *testing.T) {
			response, err := c.CurrentEpoch(context.Background(), &pb.CurrentEpochRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(2), response.Epochnum.Value)
		}},
		{"NetId", func(t *testing.T) {
			response, err := c.NetID(context.Background(), &pb.NetIDRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(networkID), response.Netid.Value)
		}},
		{"LayerDuration", func(t *testing.T) {
			response, err := c.LayerDuration(context.Background(), &pb.LayerDurationRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(layerDurationSec), response.Duration.Value)
		}},
		{"MaxTransactionsPerSecond", func(t *testing.T) {
			response, err := c.MaxTransactionsPerSecond(context.Background(), &pb.MaxTransactionsPerSecondRequest{})
			require.NoError(t, err)
			require.Equal(t, uint64(layerAvgSize*txsPerBlock/layerDurationSec), response.Maxtxpersecond.Value)
		}},
		{"AccountMeshDataQuery", func(t *testing.T) {
			subtests := []struct {
				name string
				run  func(*testing.T)
			}{
				{
					// all inputs default to zero, no filter
					// query is valid but MaxResults is 0 so expect no results
					name: "no inputs",
					run: func(t *testing.T) {
						_, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{})
						require.Error(t, err, "expected an error")
						require.Contains(t, err.Error(), "`Filter` must be provided")
						statusCode := status.Code(err)
						require.Equal(t, codes.InvalidArgument, statusCode)
					},
				},
				{
					name: "MinLayer too high",
					run: func(t *testing.T) {
						_, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MinLayer: layerCurrent + 1,
						})
						require.Error(t, err, "expected an error")
						require.Contains(t, err.Error(), "`LatestLayer` must be less than")
						statusCode := status.Code(err)
						require.Equal(t, codes.InvalidArgument, statusCode)
					},
				},
				{
					// This does not produce an error but we expect no results
					name: "Offset too high",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{},
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
					name: "no filter",
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
					name: "empty filter",
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
					name: "filter with empty AccountId",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(0), res.TotalResults)
						require.Equal(t, 0, len(res.Data))
					},
				},
				{
					name: "filter with valid AccountId",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS),
								AccountId:            &pb.AccountId{Address: addr1.Bytes()},
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(1), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
					},
				},
				{
					name: "filter with valid AccountId and AccountMeshDataFlags zero",
					run: func(t *testing.T) {
						_, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: addr1.Bytes()},
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
					name: "filter with valid AccountId and AccountMeshDataFlags tx only",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: addr1.Bytes()},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(1), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
						checkAccountMeshDataItemTx(t, res.Data[0].Item)
					},
				},
				{
					name: "filter with valid AccountId and AccountMeshDataFlags activations only",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(10),
							Filter: &pb.AccountMeshDataFilter{
								AccountId:            &pb.AccountId{Address: addr1.Bytes()},
								AccountMeshDataFlags: uint32(pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(1), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
						checkAccountMeshDataItemActivation(t, res.Data[0].Item)
					},
				},
				{
					name: "filter with valid AccountId and AccountMeshDataFlags all",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							// Zero means unlimited
							MaxResults: uint32(0),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{Address: addr1.Bytes()},
								AccountMeshDataFlags: uint32(
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
										pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(2), res.TotalResults)
						require.Equal(t, 2, len(res.Data))
						checkAccountMeshDataItemTx(t, res.Data[0].Item)
						checkAccountMeshDataItemActivation(t, res.Data[1].Item)
					},
				},
				{
					name: "max results",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(1),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{Address: addr1.Bytes()},
								AccountMeshDataFlags: uint32(
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
										pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(2), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
						checkAccountMeshDataItemTx(t, res.Data[0].Item)
					},
				},
				{
					name: "max results page 2",
					run: func(t *testing.T) {
						res, err := c.AccountMeshDataQuery(context.Background(), &pb.AccountMeshDataQueryRequest{
							MaxResults: uint32(1),
							Offset:     uint32(1),
							Filter: &pb.AccountMeshDataFilter{
								AccountId: &pb.AccountId{Address: addr1.Bytes()},
								AccountMeshDataFlags: uint32(
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
										pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
							},
						})
						require.NoError(t, err)
						require.Equal(t, uint32(2), res.TotalResults)
						require.Equal(t, 1, len(res.Data))
						checkAccountMeshDataItemActivation(t, res.Data[0].Item)
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
					name: "missing filter",
					run:  generateRunFnError("`Filter` must be provided", &pb.AccountMeshDataStreamRequest{}),
				},
				{
					name: "empty filter",
					run: generateRunFnError("`Filter.AccountId` must be provided", &pb.AccountMeshDataStreamRequest{
						Filter: &pb.AccountMeshDataFilter{},
					}),
				},
				{
					name: "missing address",
					run: generateRunFnError("`Filter.AccountId` must be provided", &pb.AccountMeshDataStreamRequest{
						Filter: &pb.AccountMeshDataFilter{
							AccountMeshDataFlags: uint32(
								pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
									pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
						},
					}),
				},
				{
					name: "filter with zero flags",
					run: generateRunFnError("`Filter.AccountMeshDataFlags` must set at least one bitfield", &pb.AccountMeshDataStreamRequest{
						Filter: &pb.AccountMeshDataFilter{
							AccountId:            &pb.AccountId{Address: addr1.Bytes()},
							AccountMeshDataFlags: uint32(0),
						},
					}),
				},

				// SUCCESS
				{
					name: "empty address",
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
					name: "invalid address",
					run: generateRunFn(&pb.AccountMeshDataStreamRequest{
						Filter: &pb.AccountMeshDataFilter{
							AccountId: &pb.AccountId{Address: []byte{'A'}},
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
					name: "end layer after current layer",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: uint32(layerCurrent),
						EndLayer:   uint32(layerCurrent + 2),
					}),
				},

				// start layer after current layer
				{
					name: "start layer after current layer",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: uint32(layerCurrent + 2),
						EndLayer:   uint32(layerCurrent + 3),
					}),
				},

				// layer after last received
				{
					name: "layer after last received",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: uint32(layerLatest + 1),
						EndLayer:   uint32(layerLatest + 2),
					}),
				},

				// very very large range
				{
					name: "very very large range",
					run: generateRunFnError("error retrieving layer data", &pb.LayersQueryRequest{
						StartLayer: uint32(0),
						EndLayer:   uint32(math.MaxUint32),
					}),
				},

				// GOOD INPUTS

				// nil inputs
				// not an error since these default to zero, see
				// https://github.com/spacemeshos/api/issues/87
				{
					name: "nil inputs",
					run:  generateRunFn(1, &pb.LayersQueryRequest{}),
				},

				// start layer after end layer: expect no error, zero results
				{
					name: "start layer after end layer",
					run: generateRunFn(0, &pb.LayersQueryRequest{
						StartLayer: uint32(layerCurrent + 1),
						EndLayer:   uint32(layerCurrent),
					}),
				},

				// same start/end layer: expect no error, one result
				{
					name: "same start end layer",
					run: generateRunFn(1, &pb.LayersQueryRequest{
						StartLayer: uint32(layerVerified),
						EndLayer:   uint32(layerVerified),
					}),
				},

				// start layer after last approved/confirmed layer (but before current layer)
				{
					name: "start layer after last approved confirmed layer",
					run: generateRunFn(2, &pb.LayersQueryRequest{
						StartLayer: uint32(layerVerified + 1),
						EndLayer:   uint32(layerVerified + 2),
					}),
				},

				// end layer after last approved/confirmed layer (but before current layer)
				{
					name: "end layer after last approved confirmed layer",
					// expect difference + 1 return layers
					run: generateRunFn(int(layerVerified)+2-layerFirst+1, &pb.LayersQueryRequest{
						StartLayer: uint32(layerFirst),
						EndLayer:   uint32(layerVerified + 2),
					}),
				},

				// comprehensive valid test
				{
					name: "comprehensive",
					run: func(t *testing.T) {
						req := &pb.LayersQueryRequest{
							StartLayer: uint32(layerFirst),
							EndLayer:   uint32(layerLatest),
						}

						res, err := c.LayersQuery(context.Background(), req)
						require.NoError(t, err, "query returned unexpected error")

						// endpoint inclusive so add one
						numLayers := layerLatest - layerFirst + 1
						require.Equal(t, numLayers, len(res.Layer))
						checkLayer(t, res.Layer[0])

						resLayerNine := res.Layer[9]
						require.Equal(t, uint64(9), resLayerNine.Number, "layer nine is ninth")
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

func checkLayer(t *testing.T, l *pb.Layer) {
	require.Equal(t, uint64(0), l.Number, "first layer is zero")
	require.Equal(t, pb.Layer_LAYER_STATUS_CONFIRMED, l.Status, "first layer is confirmed")

	require.Equal(t, atxPerLayer, len(l.Activations), "unexpected number of activations in layer")
	require.Equal(t, blkPerLayer, len(l.Blocks), "unexpected number of blocks in layer")
	require.Equal(t, stateRoot.Bytes(), l.RootStateHash, "unexpected state root")

	data, err := globalAtx.InnerBytes()
	require.NoError(t, err, "error getting activation data size")

	// The order of the activations is not deterministic since they're
	// stored in a map, and randomized each run. Check if either matches.
	require.Condition(t, func() bool {
		for _, a := range l.Activations {
			// Compare the two element by element
			if a.Layer != globalAtx.PubLayerID.Uint64() {
				continue
			}
			if bytes.Compare(a.Id.Id, globalAtx.ID().Bytes()) != 0 {
				continue
			}
			if bytes.Compare(a.SmesherId.Id, globalAtx.NodeID.ToBytes()) != 0 {
				continue
			}
			if bytes.Compare(a.Coinbase.Address, globalAtx.Coinbase.Bytes()) != 0 {
				continue
			}
			if bytes.Compare(a.PrevAtx.Id, globalAtx.PrevATXID.Bytes()) != 0 {
				continue
			}
			if a.CommitmentSize != uint64(len(data)) {
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
	require.Equal(t, block1.ID().Bytes(), resBlock.Id)

	// Check the tx as well
	resTx := resBlock.Transactions[0]
	require.Equal(t, globalTx.ID().Bytes(), resTx.Id.Id)
	require.Equal(t, globalTx.Origin().Bytes(), resTx.Sender.Address)
	require.Equal(t, globalTx.GasLimit, resTx.GasOffered.GasProvided)
	require.Equal(t, globalTx.Amount, resTx.Amount.Value)
	require.Equal(t, globalTx.AccountNonce, resTx.Counter)
	require.Equal(t, globalTx.Signature[:], resTx.Signature.Signature)
	require.Equal(t, pb.Signature_SCHEME_ED25519_PLUS_PLUS, resTx.Signature.Scheme)
	require.Equal(t, globalTx.Origin().Bytes(), resTx.Signature.PublicKey)

	// The Data field is a bit trickier to read
	switch x := resTx.Data.(type) {
	case *pb.Transaction_CoinTransfer:
		require.Equal(t, globalTx.Recipient.Bytes(), x.CoinTransfer.Receiver.Address,
			"inner coin transfer tx has bad recipient")
	default:
		require.Fail(t, "inner tx has wrong tx data type")
	}
}

func TestAccountMeshDataStream_comprehensive(t *testing.T) {
	// TODO: Re-enable this test in phase9 as the Reporter has been fixed.
	// This will fail for now because the Reporter does not block.
	t.Skip()

	grpcService := NewMeshService(&networkMock, txAPI, txMempool, &genTime, &SyncerMock{}, layersPerEpoch, networkID, layerDurationSec, layerAvgSize, txsPerBlock)
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.NewGrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c := pb.NewMeshServiceClient(conn)

	// set up the grpc listener stream
	req := &pb.AccountMeshDataStreamRequest{
		Filter: &pb.AccountMeshDataFilter{
			AccountId: &pb.AccountId{Address: addr1.Bytes()},
			AccountMeshDataFlags: uint32(
				pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_ACTIVATIONS |
					pb.AccountMeshDataFlag_ACCOUNT_MESH_DATA_FLAG_TRANSACTIONS),
		},
	}

	// Need to wait for goroutine to end before ending the test
	wg := sync.WaitGroup{}
	wg.Add(1)

	// This will block so run it in a goroutine
	go func() {
		defer wg.Done()
		stream, err := c.AccountMeshDataStream(context.Background(), req)
		require.NoError(t, err, "stream request returned unexpected error")

		var res *pb.AccountMeshDataStreamResponse

		res, err = stream.Recv()
		require.NoError(t, err, "got error from stream")
		checkAccountMeshDataItemTx(t, res.Data.Item)

		// second item should be an activation
		res, err = stream.Recv()
		require.NoError(t, err, "got error from stream")
		checkAccountMeshDataItemActivation(t, res.Data.Item)

		// look for EOF
		// third and fourth events streamed should not be received! they should be
		// filtered out
		res, err = stream.Recv()
		require.Equal(t, io.EOF, err, "expected EOF from stream")
	}()

	// initialize the streamer
	log.Info("initializing event stream")
	events.InitializeEventReporterWithOptions("", 0, true)

	// publish a tx
	events.ReportNewTx(globalTx)

	// publish an activation
	events.ReportNewActivation(globalAtx)

	// test streaming a tx and an atx that are filtered out
	// these should not be received
	events.ReportNewTx(globalTx2)
	events.ReportNewActivation(globalAtx2)

	// close the stream
	log.Info("closing event stream")
	events.CloseEventReporter()

	// wait for the goroutine
	wg.Wait()
}

func TestAccountDataStream_comprehensive(t *testing.T) {
	svc := NewGlobalStateService(&networkMock, txAPI, &genTime, &SyncerMock{})
	shutDown := launchServer(t, svc)
	defer shutDown()

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.NewGrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c := pb.NewGlobalStateServiceClient(conn)

	// set up the grpc listener stream
	req := &pb.AccountDataStreamRequest{
		Filter: &pb.AccountDataFilter{
			AccountId: &pb.AccountId{Address: addr1.Bytes()},
			AccountDataFlags: uint32(
				pb.AccountDataFlag_ACCOUNT_DATA_FLAG_REWARD |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_ACCOUNT |
					pb.AccountDataFlag_ACCOUNT_DATA_FLAG_TRANSACTION_RECEIPT),
		},
	}

	// Synchronize the two routines
	wg := sync.WaitGroup{}
	wg.Add(1)

	// This will block so run it in a goroutine
	go func() {
		defer wg.Done()
		stream, err := c.AccountDataStream(context.Background(), req)
		require.NoError(t, err, "stream request returned unexpected error")

		var res *pb.AccountDataStreamResponse

		res, err = stream.Recv()
		require.NoError(t, err, "got error from stream")
		checkAccountDataItemReceipt(t, res.Data.Item)

		res, err = stream.Recv()
		require.NoError(t, err, "got error from stream")
		checkAccountDataItemReward(t, res.Data.Item)

		res, err = stream.Recv()
		require.NoError(t, err, "got error from stream")
		checkAccountDataItemAccount(t, res.Data.Item)

		// look for EOF
		// the next two events streamed should not be received! they should be
		// filtered out
		res, err = stream.Recv()
		require.Equal(t, io.EOF, err, "expected EOF from stream")
	}()

	// initialize the streamer
	log.Info("initializing event stream")
	events.InitializeEventReporterWithOptions("", 0, true)

	// publish a receipt
	events.ReportReceipt(events.TxReceipt{
		Address: addr1,
	})

	// publish a reward
	events.ReportRewardReceived(events.Reward{
		Layer:       layerFirst,
		Total:       rewardAmount,
		LayerReward: rewardAmount * 2,
		Coinbase:    addr1,
	})

	// publish an account data update
	events.ReportAccountUpdate(addr1)

	// test streaming a reward and account update that should be filtered out
	// these should not be received
	events.ReportAccountUpdate(addr2)
	events.ReportRewardReceived(events.Reward{Coinbase: addr2})

	// close the stream
	log.Info("closing event stream")
	events.CloseEventReporter()

	// wait for the goroutine to finish
	wg.Wait()
}

func TestLayerStream_comprehensive(t *testing.T) {
	// TODO: Re-enable this test in phase9 as the Reporter has been fixed.
	// This will fail for now because the Reporter does not block.
	t.Skip()

	grpcService := NewMeshService(&networkMock, txAPI, txMempool, &genTime, &SyncerMock{}, layersPerEpoch, networkID, layerDurationSec, layerAvgSize, txsPerBlock)
	shutDown := launchServer(t, grpcService)
	defer shutDown()

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.NewGrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c := pb.NewMeshServiceClient(conn)

	// set up the grpc listener stream
	req := &pb.LayerStreamRequest{}

	// Need to wait for goroutine to end before ending the test
	wg := sync.WaitGroup{}
	wg.Add(1)

	// This will block so run it in a goroutine
	go func() {
		defer wg.Done()
		stream, err := c.LayerStream(context.Background(), req)
		require.NoError(t, err, "stream request returned unexpected error")

		var res *pb.LayerStreamResponse

		res, err = stream.Recv()
		require.NoError(t, err, "got error from stream")
		require.Equal(t, uint64(0), res.Layer.Number)
		require.Equal(t, events.LayerStatusTypeConfirmed, res.Layer.Status)
		checkLayer(t, res.Layer)

		// look for EOF
		_, err = stream.Recv()
		require.Equal(t, io.EOF, err, "expected EOF from stream")
	}()

	// initialize the streamer
	log.Info("initializing event stream")
	events.InitializeEventReporter("")

	layer, err := txAPI.GetLayer(0)
	require.NoError(t, err)
	events.ReportNewLayer(events.NewLayer{
		Layer:  layer,
		Status: events.LayerStatusTypeConfirmed,
	})

	// close the stream
	log.Info("closing event stream")
	events.CloseEventReporter()

	// wait for the goroutine
	wg.Wait()
}

func checkAccountDataQueryItemAccount(t *testing.T, dataItem interface{}) {
	switch x := dataItem.(type) {
	case *pb.AccountData_AccountWrapper:
		// Check the account, nonce, and balance
		require.Equal(t, addr1.Bytes(), x.AccountWrapper.AccountId.Address,
			"inner account has bad address")
		require.Equal(t, uint64(accountCounter), x.AccountWrapper.Counter,
			"inner account has bad counter")
		require.Equal(t, uint64(accountBalance), x.AccountWrapper.Balance.Value,
			"inner account has bad balance")
	default:
		require.Fail(t, "inner account data item has wrong data type")
	}
}

func checkAccountDataQueryItemReward(t *testing.T, dataItem interface{}) {
	switch x := dataItem.(type) {
	case *pb.AccountData_Reward:
		require.Equal(t, uint64(layerFirst), x.Reward.Layer)
		require.Equal(t, uint64(rewardAmount), x.Reward.Total.Value)
		require.Equal(t, uint64(rewardAmount), x.Reward.LayerReward.Value)
		require.Equal(t, addr1.Bytes(), x.Reward.Coinbase.Address)
	default:
		require.Fail(t, "inner account data item has wrong data type")
	}
}

func checkAccountMeshDataItemTx(t *testing.T, dataItem interface{}) {
	switch x := dataItem.(type) {
	case *pb.AccountMeshData_Transaction:
		// Check the sender
		require.Equal(t, globalTx.Origin().Bytes(), x.Transaction.Signature.PublicKey,
			"inner coin transfer tx has bad sender")
		require.Equal(t, globalTx.Amount, x.Transaction.Amount.Value,
			"inner coin transfer tx has bad amount")
		require.Equal(t, globalTx.AccountNonce, x.Transaction.Counter,
			"inner coin transfer tx has bad counter")

		// Need to further check tx type
		switch y := x.Transaction.Data.(type) {
		case *pb.Transaction_CoinTransfer:
			require.Equal(t, globalTx.Recipient.Bytes(), y.CoinTransfer.Receiver.Address,
				"inner coin transfer tx has bad recipient")
		default:
			require.Fail(t, "inner tx has wrong tx data type")
		}
	default:
		require.Fail(t, "inner account data item has wrong data type")
	}
}

func checkAccountMeshDataItemActivation(t *testing.T, dataItem interface{}) {
	switch x := dataItem.(type) {
	case *pb.AccountMeshData_Activation:
		require.Equal(t, globalAtx.ID().Bytes(), x.Activation.Id.Id)
		require.Equal(t, globalAtx.PubLayerID.Uint64(), x.Activation.Layer)
		require.Equal(t, globalAtx.NodeID.ToBytes(), x.Activation.SmesherId.Id)
		require.Equal(t, globalAtx.Coinbase.Bytes(), x.Activation.Coinbase.Address)
		require.Equal(t, globalAtx.PrevATXID.Bytes(), x.Activation.PrevAtx.Id)
		data, err := globalAtx.InnerBytes()
		require.NoError(t, err)
		require.Equal(t, uint64(len(data)), x.Activation.CommitmentSize)
	default:
		require.Fail(t, "inner account data item has wrong tx data type")
	}
}

func checkAccountDataItemReward(t *testing.T, dataItem interface{}) {
	switch x := dataItem.(type) {
	case *pb.AccountData_Reward:
		require.Equal(t, uint64(rewardAmount), x.Reward.Total.Value)
		require.Equal(t, uint64(layerFirst), x.Reward.Layer)
		require.Equal(t, uint64(rewardAmount*2), x.Reward.LayerReward.Value)
		require.Equal(t, addr1.Bytes(), x.Reward.Coinbase.Address)

	default:
		require.Fail(t, "inner account data item has wrong data type")
	}
}

func checkAccountDataItemReceipt(t *testing.T, dataItem interface{}) {
	switch x := dataItem.(type) {
	case *pb.AccountData_Receipt:
		require.Equal(t, addr1.Bytes(), x.Receipt.AppAddress.Address)

	default:
		require.Fail(t, "inner account data item has wrong data type")
	}
}

func checkAccountDataItemAccount(t *testing.T, dataItem interface{}) {
	switch x := dataItem.(type) {
	case *pb.AccountData_AccountWrapper:
		require.Equal(t, addr1.Bytes(), x.AccountWrapper.AccountId.Address)
		require.Equal(t, uint64(accountBalance), x.AccountWrapper.Balance.Value)
		require.Equal(t, uint64(accountCounter), x.AccountWrapper.Counter)

	default:
		require.Fail(t, "inner account data item has wrong data type")
	}
}

func TestMultiService(t *testing.T) {
	svc1 := NewNodeService(&networkMock, txAPI, &genTime, &SyncerMock{})
	svc2 := NewMeshService(&networkMock, txAPI, txMempool, &genTime, &SyncerMock{}, layersPerEpoch, networkID, layerDurationSec, layerAvgSize, txsPerBlock)
	shutDown := launchServer(t, svc1, svc2)
	defer shutDown()

	// start a client
	addr := "localhost:" + strconv.Itoa(cfg.NewGrpcServerPort)

	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, conn.Close())
	}()
	c1 := pb.NewNodeServiceClient(conn)
	c2 := pb.NewMeshServiceClient(conn)

	// call endpoints and validate results
	const message = "Hello World"
	res1, err1 := c1.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message}})
	require.NoError(t, err1)
	require.Equal(t, message, res1.Msg.Value)
	res2, err2 := c2.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
	require.NoError(t, err2)
	require.Equal(t, uint64(genTime.GetGenesisTime().Unix()), res2.Unixtime.Value)

	// Make sure that shutting down the grpc service shuts them both down
	shutDown()

	// Make sure NodeService is off
	res1, err1 = c1.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message}})
	require.Error(t, err1)
	require.Contains(t, err1.Error(), "rpc error: code = Unavailable")

	// Make sure MeshService is off
	res2, err2 = c2.GenesisTime(context.Background(), &pb.GenesisTimeRequest{})
	require.Error(t, err2)
	require.Contains(t, err2.Error(), "rpc error: code = Unavailable")
}

func TestJsonApi(t *testing.T) {
	const message = "hello world!"

	// we cannot start the gateway service without enabling at least one service
	require.Equal(t, cfg.StartNodeService, false)
	require.Equal(t, cfg.StartMeshService, false)
	shutDown := launchServer(t)
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", cfg.NewJSONServerPort, "v1/node/echo")
	t.Log("sending POST request to", url)
	_, err := http.Post(url, "application/json", strings.NewReader(payload))
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf(
		"dial tcp 127.0.0.1:%d: connect: connection refused",
		cfg.NewJSONServerPort))
	shutDown()

	// enable services and try again
	svc1 := NewNodeService(&networkMock, txAPI, &genTime, &SyncerMock{})
	svc2 := NewMeshService(&networkMock, txAPI, txMempool, &genTime, &SyncerMock{}, layersPerEpoch, networkID, layerDurationSec, layerAvgSize, txsPerBlock)
	cfg.StartNodeService = true
	cfg.StartMeshService = true
	shutDown = launchServer(t, svc1, svc2)
	defer shutDown()

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
