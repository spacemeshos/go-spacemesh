package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/api"
	apiCfg "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/poet/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"os"
	"path/filepath"
	"strconv"
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
	cfg, err := integration.DefaultConfig()
	if err != nil {
		return nil, err
	}
	cfg.NodeAddress = "127.0.0.1:9091"
	cfg.InitialRoundDuration = time.Duration(35 * time.Second).String()

	h, err := integration.NewHarness(cfg)
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
	poetClient, err := NewRPCPoetHarnessClient()
	r.NoError(err)
	suite.poetCleanup = poetClient.CleanUp
	rng := BLS381.DefaultSeed()
	for i := 0; i < numOfInstances; i++ {
		smApp := NewSpacemeshApp()

		smApp.Config.POST = nipst.DefaultConfig()
		smApp.Config.POST.Difficulty = 5
		smApp.Config.POST.NumProvenLabels = 10
		smApp.Config.POST.SpacePerUnit = 1 << 10 // 1KB.
		smApp.Config.POST.FileSize = 1 << 10     // 1KB.

		smApp.Config.HARE.N = numOfInstances
		smApp.Config.HARE.F = numOfInstances / 2
		smApp.Config.HARE.RoundDuration = 3
		smApp.Config.HARE.WakeupDelta = 10
		smApp.Config.HARE.ExpectedLeaders = 5
		smApp.Config.CoinbaseAccount = strconv.Itoa(i + 1)
		smApp.Config.LayerAvgSize = numOfInstances
		smApp.Config.LayersPerEpoch = 3

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

		postClient := nipst.NewPostClient(&smApp.Config.POST)

		err = smApp.initServices(nodeID, swarm, dbStorepath, edSgn, false, hareOracle, uint32(layerSize), postClient, poetClient, vrfSigner, uint32(smApp.Config.CONSENSUS.LayersPerEpoch))
		r.NoError(err)
		smApp.setupGenesis()

		suite.apps = append(suite.apps, smApp)
		suite.dbs = append(suite.dbs, dbStorepath)
		runningName++
	}
	activateGrpcServer(suite.apps[0])
}

func activateGrpcServer(smApp *SpacemeshApp) {
	smApp.Config.API.StartGrpcServer = true
	smApp.grpcAPIService = api.NewGrpcService(smApp.P2P, smApp.state, smApp.txProcessor)
	smApp.grpcAPIService.StartService(nil)
}

func (suite *AppTestSuite) TestMultipleNodes() {
	//EntryPointCreated <- true

	const numberOfEpochs = 2 // first 2 epochs are genesis
	//addr := address.BytesToAddress([]byte{0x01})
	dst := address.BytesToAddress([]byte{0x02})
	acc1Signer, err := signing.NewEdSignerFromBuffer(common.FromHex(apiCfg.Account1Private))
	if err != nil {
		log.Panic("Could not build ed signer err=%v", err)
	}
	tx, err := types.NewSignedTx(0, dst, 10, 1, 1, acc1Signer)
	if err != nil {
		log.Panic("panicked creating signed tx err=%v", err)
	}
	txbytes, _ := types.SignedTransactionAsBytes(tx)
	path := "../tmp/test/state_" + time.Now().String()
	suite.initMultipleInstances(5, path)
	for _, a := range suite.apps {
		a.startServices()
	}

	defer suite.gracefulShutdown()

	_ = suite.apps[0].P2P.Broadcast(miner.IncomingTxProtocol, txbytes)
	timeout := time.After(4.5 * 60 * time.Second)

	stickyClientsDone := 0
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			suite.T().Fatal("timed out")
		default:
			maxClientsDone := 0
			for idx, app := range suite.apps {
				if 10 == app.state.GetBalance(dst) &&
					uint32(suite.apps[idx].mesh.LatestLayer()) == numberOfEpochs*uint32(suite.apps[idx].Config.LayersPerEpoch)+1 { // make sure all had 1 non-genesis layer

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
									break loop
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
			time.Sleep(30 * time.Second)
		}
	}

	suite.validateBlocksAndATXs(8)

}

func (suite *AppTestSuite) validateBlocksAndATXs(untilLayer types.LayerID) {

	type nodeData struct {
		layertoblocks map[types.LayerID][]types.BlockID
		atxPerEpoch   map[types.EpochId][]types.AtxId
	}

	datamap := make(map[string]*nodeData)

	// wait until all nodes are in `untilLayer`
	for {

		count := 0
		for _, ap := range suite.apps {
			curNodeLastLayer := ap.blockListener.ValidatedLayer()
			if curNodeLastLayer < untilLayer {
				log.Info("layer for %v was %v, want %v", ap.nodeId.Key, curNodeLastLayer, 8)
			} else {
				count++
			}
		}

		if count == len(suite.apps) {
			break
		}

		time.Sleep(time.Duration(suite.apps[0].Config.LayerDurationSec/2) * time.Second)
	}

	for _, ap := range suite.apps {
		if _, ok := datamap[ap.nodeId.Key]; !ok {
			datamap[ap.nodeId.Key] = new(nodeData)
			datamap[ap.nodeId.Key].atxPerEpoch = make(map[types.EpochId][]types.AtxId)
			datamap[ap.nodeId.Key].layertoblocks = make(map[types.LayerID][]types.BlockID)
		}

		for i := types.LayerID(0); i <= untilLayer; i++ {
			lyr, err := ap.blockListener.GetLayer(i)
			if err != nil {
				log.Error("ERROR: couldn't get a validated layer from db layer %v, %v", i, err)
			}
			for _, b := range lyr.Blocks() {
				datamap[ap.nodeId.Key].layertoblocks[lyr.Index()] = append(datamap[ap.nodeId.Key].layertoblocks[lyr.Index()], b.ID())
			}
			epoch := lyr.Index().GetEpoch(uint16(ap.Config.LayersPerEpoch))
			if _, ok := datamap[ap.nodeId.Key].atxPerEpoch[epoch]; !ok {
				atxs, err := ap.blockListener.AtxDB.GetEpochAtxIds(epoch)
				if err != nil {
					log.Error("ERROR: couldn't get atxs for passed epoch: %v, err: %v", epoch, err)
				}
				datamap[ap.nodeId.Key].atxPerEpoch[epoch] = atxs
			}
		}
	}

	for i, d := range datamap {
		log.Info("Node %v in len(layerstoblocks) %v", i, len(d.layertoblocks))
		for i2, d2 := range datamap {
			if i == i2 {
				continue
			}
			assert.Equal(suite.T(), len(d.layertoblocks), len(d2.layertoblocks), "%v has not matching layer to %v. %v not %v", i, i2, len(d.layertoblocks), len(d2.layertoblocks))

			for l, bl := range d.layertoblocks {
				assert.Equal(suite.T(), len(bl), len(d2.layertoblocks[l]),
					fmt.Sprintf("%v and %v had different block maps for layer: %v: %v: %v \r\n %v: %v", i, i2, l, i, bl, i2, d2.layertoblocks[l]))
			}

			for e, atx := range d.atxPerEpoch {
				assert.Equal(suite.T(), len(atx), len(d2.atxPerEpoch[e]),
					fmt.Sprintf("%v and %v had different atx maps for epoch: %v: %v: %v \r\n %v: %v", i, i2, e, i, atx, i2, d2.atxPerEpoch[e]))
			}
		}
	}

	// assuming all nodes have the same results
	layers_per_epoch := int(suite.apps[0].Config.LayersPerEpoch)
	layer_avg_size := suite.apps[0].Config.LayerAvgSize
	patient := datamap[suite.apps[0].nodeId.Key]

	lastlayer := len(patient.layertoblocks)

	total_blocks := 0
	for _, l := range patient.layertoblocks {
		total_blocks += len(l)
	}

	first_epoch_blocks := 0

	for i := 0; i < layers_per_epoch; i++ {
		if l, ok := patient.layertoblocks[types.LayerID(i)]; ok {
			first_epoch_blocks += len(l)
		}
	}

	total_atxs := 0

	for _, atxs := range patient.atxPerEpoch {
		total_atxs += len(atxs)
	}

	assert.True(suite.T(), ((total_blocks-first_epoch_blocks)/(lastlayer-layers_per_epoch)) == layer_avg_size, fmt.Sprintf("not good num of blocks got: %v, want: %v. total_blocks: %v, first_epoch_blocks: %v, lastlayer: %v, layers_per_epoch: %v", (total_blocks-first_epoch_blocks)/(lastlayer-layers_per_epoch), layer_avg_size, total_blocks, first_epoch_blocks, lastlayer, layers_per_epoch))
	assert.True(suite.T(), total_atxs == (lastlayer/layers_per_epoch)*len(suite.apps), fmt.Sprintf("not good num of atxs got: %v, want: %v", total_atxs, (lastlayer/layers_per_epoch)*len(suite.apps)))

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
		wg.Add(1)
		go func(app *SpacemeshApp) {
			app.stopServices()
			wg.Done()
		}(app)
	}
	wg.Wait()
}

func TestAppTestSuite(t *testing.T) {
	//defer leaktest.Check(t)()
	suite.Run(t, new(AppTestSuite))
}
