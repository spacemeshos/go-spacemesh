package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/api"
	apiCfg "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/poet/integration"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type AppTestSuite struct {
	suite.Suite

	apps        []*SpacemeshApp
	dbs         []string
	poetCleanup func() error
}

func (suite *AppTestSuite) SetupTest() {
	suite.apps = make([]*SpacemeshApp, 0, 0)
	suite.dbs = make([]string, 0, 0)
}

// NewRPCPoetHarnessClient returns a new instance of RPCPoetClient
// which utilizes a local self-contained poet server instance
// in order to exercise functionality.
func NewRPCPoetHarnessClient() (*nipst.RPCPoetClient, error) {
	h, err := integration.NewHarness("127.0.0.1:9091")
	if err != nil {
		return nil, err
	}

	return nipst.NewRPCPoetClient(h.PoetClient, h.TearDown), nil
}

func (suite *AppTestSuite) TearDownTest() {
	if err := suite.poetCleanup(); err != nil {
		log.Error("error while cleaning up PoET: %v", err)
	}
	for _, dbinst := range suite.dbs {
		if err := os.RemoveAll(dbinst); err != nil {
			panic(fmt.Sprintf("what happened : %v", err))
		}
	}
	if err := os.RemoveAll("../tmp"); err != nil {
		log.Error("error while cleaning up tmp dir: %v", err)
	}
	//poet should clean up after himself
	if matches, err := filepath.Glob("*.bin"); err != nil {
		log.Error("error while finding PoET bin files: %v", err)
	} else {
		for _, f := range matches {
			if err = os.Remove(f); err != nil {
				log.Error("error while cleaning up PoET bin files: %v", err)
			}
		}
	}
}

func (suite *AppTestSuite) initMultipleInstances(numOfInstances int, storeFormat string) {
	r := require.New(suite.T())

	net := service.NewSimulator()
	runningName := 'a'
	rolacle := eligibility.New()
	poet, err := NewRPCPoetHarnessClient()
	r.NoError(err)
	suite.poetCleanup = poet.CleanUp
	rng := BLS381.DefaultSeed()
	for i := 0; i < numOfInstances; i++ {
		smApp := NewSpacemeshApp()
		smApp.Config.HARE.N = numOfInstances
		smApp.Config.HARE.F = numOfInstances / 2
		smApp.Config.HARE.WakeupDelta = 15
		smApp.Config.HARE.ExpectedLeaders = 5

		edSgn := signing.NewEdSigner()
		pub := edSgn.PublicKey()

		r.NoError(err)
		vrfPriv, vrfPub := BLS381.GenKeyPair(rng)
		vrfSigner := BLS381.NewBlsSigner(vrfPriv)
		nodeID := types.NodeId{Key: pub.String(), VRFPublicKey: vrfPub}

		swarm := net.NewNode()
		dbStorepath := storeFormat + string(runningName)

		hareOracle := oracle.NewLocalOracle(rolacle, numOfInstances, nodeID)
		hareOracle.Register(true, pub.String())

		layerSize := numOfInstances
		npstCfg := nipst.PostParams{
			Difficulty:           5,
			NumberOfProvenLabels: 10,
			SpaceUnit:            1024,
		}
		err = smApp.initServices(nodeID, swarm, dbStorepath, edSgn, false, hareOracle, uint32(layerSize), nipst.NewPostClient(), poet, vrfSigner, npstCfg, 3)
		r.NoError(err)
		smApp.setupGenesis(apiCfg.DefaultGenesisConfig())

		suite.apps = append(suite.apps, smApp)
		suite.dbs = append(suite.dbs, dbStorepath)
		runningName++
	}
	activateGrpcServer(suite.apps[0])
}

func activateGrpcServer(smApp *SpacemeshApp) {
	smApp.Config.API.StartGrpcServer = true
	smApp.grpcAPIService = api.NewGrpcService(smApp.P2P, smApp.state)
	smApp.grpcAPIService.StartService(nil)
}

func (suite *AppTestSuite) TestMultipleNodes() {
	//EntryPointCreated <- true

	addr := address.BytesToAddress([]byte{0x01})
	dst := address.BytesToAddress([]byte{0x02})
	tx := types.SerializableTransaction{}
	tx.Amount = big.NewInt(10).Bytes()
	tx.GasLimit = 1
	tx.Origin = addr
	tx.Recipient = &dst
	tx.Price = big.NewInt(1).Bytes()

	txbytes, _ := types.TransactionAsBytes(&tx)
	path := "../tmp/test/state_" + time.Now().String()
	suite.initMultipleInstances(5, path)
	for _, a := range suite.apps {
		a.startServices()
	}

	_ = suite.apps[0].P2P.Broadcast(miner.IncomingTxProtocol, txbytes)
	timeout := time.After(4.5 * 60 * time.Second)

	stickyClientsDone := 0
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			suite.T().Fatal("timed out")
		default:
			maxClientsDone := 0
			for idx, app := range suite.apps {
				if big.NewInt(10).Cmp(app.state.GetBalance(dst)) == 0 &&
					!app.mesh.LatestLayer().GetEpoch(app.Config.CONSENSUS.LayersPerEpoch).IsGenesis() {

					suite.validateLastATXActiveSetSize(app)
					clientsDone := 0
					for idx2, app2 := range suite.apps {
						if idx != idx2 {
							r1 := app.state.IntermediateRoot(false).String()
							r2 := app2.state.IntermediateRoot(false).String()
							if r1 == r2 {
								clientsDone++
								if clientsDone == len(suite.apps)-1 {
									log.Info("%d roots confirmed out of %d", clientsDone, len(suite.apps))
									suite.gracefulShutdown()
									return
								}
							}
						}
					}
					if clientsDone > maxClientsDone {
						maxClientsDone = clientsDone
					}
				}
			}
			if maxClientsDone != stickyClientsDone {
				stickyClientsDone = maxClientsDone
				log.Info("%d roots confirmed out of %d", maxClientsDone, len(suite.apps))
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
}

func (suite *AppTestSuite) validateLastATXActiveSetSize(app *SpacemeshApp) {
	prevAtxId, err := app.atxBuilder.GetPrevAtxId(app.nodeId)
	suite.NoError(err)
	atx, err := app.mesh.GetAtx(*prevAtxId)
	suite.NoError(err)
	suite.Equal(len(suite.apps), int(atx.ActiveSetSize), "atx: %v node: %v", atx.ShortId(), app.nodeId.Key[:5])
}

func (suite *AppTestSuite) gracefulShutdown() {
	var wg sync.WaitGroup
	for _, app := range suite.apps {
		func(app SpacemeshApp) {
			wg.Add(1)
			defer wg.Done()
			app.stopServices()
		}(*app)
	}
	wg.Wait()
}

func TestAppTestSuite(t *testing.T) {
	//defer leaktest.Check(t)()
	suite.Run(t, new(AppTestSuite))
}
