package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	apiCfg "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/database"
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

func (app *AppTestSuite) SetupTest() {
	app.apps = make([]*SpacemeshApp, 0, 0)
	app.dbs = make([]string, 0, 0)
}

// NewRPCPoetHarnessClient returns a new instance of RPCPoetClient
// which utilizes a local self-contained poet server instance
// in order to exercise functionality.
func NewRPCPoetHarnessClient() (*nipst.RPCPoetClient, error) {
	h, err := integration.NewHarness()
	if err != nil {
		return nil, err
	}

	return nipst.NewRPCPoetClient(h.PoetClient, h.TearDown), nil
}

func (app *AppTestSuite) TearDownTest() {
	if err := app.poetCleanup(); err != nil {
		log.Error("error while cleaning up PoET: %v", err)
	}
	for _, dbinst := range app.dbs {
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

func (app *AppTestSuite) initMultipleInstances(numOfInstances int, storeFormat string) {
	r := require.New(app.T())

	net := service.NewSimulator()
	runningName := 'a'
	rolacle := eligibility.New()
	poet, err := NewRPCPoetHarnessClient()
	r.NoError(err)
	app.poetCleanup = poet.CleanUp
	for i := 0; i < numOfInstances; i++ {
		smApp := NewSpacemeshApp()
		smApp.Config.HARE.N = numOfInstances
		smApp.Config.HARE.F = numOfInstances / 2
		smApp.Config.HARE.WakeupDelta = 15
		smApp.Config.HARE.ExpectedLeaders = 5

		edSgn := signing.NewEdSigner()
		pub := edSgn.PublicKey()

		vrfPublicKey, vrfPrivateKey, err := crypto.GenerateVRFKeys()
		r.NoError(err)
		nodeID := types.NodeId{Key: pub.String(), VRFPublicKey: vrfPublicKey}
		vrfSigner := crypto.NewVRFSigner(vrfPrivateKey)
		swarm := net.NewNode()
		dbStorepath := storeFormat + string(runningName)

		dbStore := database.NewMemDatabase()

		hareOracle := oracle.NewLocalOracle(rolacle, numOfInstances, nodeID)
		hareOracle.Register(true, pub.String())

		layerSize := numOfInstances
		npstCfg := nipst.PostParams{
			Difficulty:           5,
			NumberOfProvenLabels: 10,
			SpaceUnit:            1024,
		}
		err = smApp.initServices(nodeID, swarm, dbStorepath, edSgn, false, hareOracle, uint32(layerSize), nipst.NewPostClient(), poet, dbStore, vrfSigner, npstCfg, 3)
		r.NoError(err)
		smApp.setupGenesis(apiCfg.DefaultGenesisConfig())

		app.apps = append(app.apps, smApp)
		app.dbs = append(app.dbs, dbStorepath)
		runningName++
	}
}

func (app *AppTestSuite) TestMultipleNodes() {
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
	app.initMultipleInstances(5, path)
	for _, a := range app.apps {
		a.startServices()
	}

	_ = app.apps[0].P2P.Broadcast(miner.IncomingTxProtocol, txbytes)
	timeout := time.After(4.5 * 60 * time.Second)

	stickyClientsDone := 0
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			app.T().Fatal("timed out")
		default:
			maxClientsDone := 0
			for idx, ap := range app.apps {
				if big.NewInt(10).Cmp(ap.state.GetBalance(dst)) == 0 &&
					!app.apps[0].mesh.LatestLayer().GetEpoch(app.apps[0].Config.CONSENSUS.LayersPerEpoch).IsGenesis() {
					clientsDone := 0
					for idx2, ap2 := range app.apps {
						if idx != idx2 {
							r1 := ap.state.IntermediateRoot(false).String()
							r2 := ap2.state.IntermediateRoot(false).String()
							if r1 == r2 {
								clientsDone++
								if clientsDone == len(app.apps)-1 {
									log.Info("%d roots confirmed out of %d", clientsDone, len(app.apps))
									app.gracefulShutdown()
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
				log.Info("%d roots confirmed out of %d", maxClientsDone, len(app.apps))
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
}

func (app *AppTestSuite) gracefulShutdown() {
	var wg sync.WaitGroup
	for _, ap := range app.apps {
		func(ap SpacemeshApp) {
			wg.Add(1)
			defer wg.Done()
			ap.stopServices()
		}(*ap)
	}
	wg.Wait()
}

func TestAppTestSuite(t *testing.T) {
	//defer leaktest.Check(t)()
	suite.Run(t, new(AppTestSuite))
}
