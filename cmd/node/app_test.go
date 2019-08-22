package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/amcl"
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
	cfg.InitialRoundDuration = time.Duration(5 * time.Second).String()

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

var net = service.NewSimulator()

func (suite *AppTestSuite) initSingleInstance(i int, genesisTime string, rng *amcl.RAND, storeFormat string, name string, rolacle *eligibility.FixedRolacle, poetClient *nipst.RPCPoetClient) {
	r := require.New(suite.T())

	smApp := NewSpacemeshApp()

	smApp.Config.POST = nipst.DefaultConfig()
	smApp.Config.POST.Difficulty = 5
	smApp.Config.POST.NumProvenLabels = 10
	smApp.Config.POST.SpacePerUnit = 1 << 10 // 1KB.
	smApp.Config.POST.FileSize = 1 << 10     // 1KB.

	smApp.Config.HARE.N = 5
	smApp.Config.HARE.F = 2
	smApp.Config.HARE.RoundDuration = 3
	smApp.Config.HARE.WakeupDelta = 5
	smApp.Config.HARE.ExpectedLeaders = 5
	smApp.Config.CoinbaseAccount = strconv.Itoa(i + 1)
	smApp.Config.LayerAvgSize = 5
	smApp.Config.LayersPerEpoch = 3
	smApp.Config.Hdist = 5
	smApp.Config.GenesisTime = genesisTime
	smApp.Config.LayerDurationSec = 20
	smApp.Config.HareEligibility.ConfidenceParam = 3
	smApp.Config.HareEligibility.EpochOffset = 0
	smApp.Config.StartMining = true

	edSgn := signing.NewEdSigner()
	pub := edSgn.PublicKey()

	vrfPriv, vrfPub := BLS381.GenKeyPair(rng)
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)
	nodeID := types.NodeId{Key: pub.String(), VRFPublicKey: vrfPub}

	swarm := net.NewNode()
	dbStorepath := storeFormat + name

	hareOracle := oracle.NewLocalOracle(rolacle, 5, nodeID)
	hareOracle.Register(true, pub.String())

	postClient := nipst.NewPostClient(&smApp.Config.POST)

	err := smApp.initServices(nodeID, swarm, dbStorepath, edSgn, false, hareOracle, uint32(smApp.Config.LayerAvgSize), postClient, poetClient, vrfSigner, uint16(smApp.Config.LayersPerEpoch))
	r.NoError(err)
	smApp.setupGenesis()

	suite.apps = append(suite.apps, smApp)
	suite.dbs = append(suite.dbs, dbStorepath)
}

func (suite *AppTestSuite) initMultipleInstances(rolacle *eligibility.FixedRolacle, rng *amcl.RAND, numOfInstances int, storeFormat string, genesisTime string, poetClient *nipst.RPCPoetClient) {
	name := 'a'
	for i := 0; i < numOfInstances; i++ {
		suite.initSingleInstance(i, genesisTime, rng, storeFormat, string(name), rolacle, poetClient)
		name++
	}
}

func activateGrpcServer(smApp *SpacemeshApp) {
	smApp.Config.API.StartGrpcServer = true
	smApp.grpcAPIService = api.NewGrpcService(smApp.P2P, smApp.state, smApp.txProcessor, smApp.atxBuilder, smApp.oracle, smApp.clock)
	smApp.grpcAPIService.StartService(nil)
}

func (suite *AppTestSuite) TestMultipleNodes() {
	//EntryPointCreated <- true
	const numberOfEpochs = 5 // first 2 epochs are genesis
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

	genesisTime := time.Now().Add(20 * time.Second).Format(time.RFC3339)

	poetClient, err := NewRPCPoetHarnessClient()
	if err != nil {
		log.Panic("failed creating poet client harness: %v", err)
	}
	suite.poetCleanup = poetClient.CleanUp

	rolacle := eligibility.New()
	rng := BLS381.DefaultSeed()

	suite.initMultipleInstances(rolacle, rng, 5, path, genesisTime, poetClient)
	for _, a := range suite.apps {
		a.startServices()
	}

	activateGrpcServer(suite.apps[0])

	startInLayer := 5 // delayed pod will start in this layer
	/*go func() {
		delay := float32(suite.apps[0].Config.LayerDurationSec) * (float32(startInLayer) + 0.5)
		<-time.After(time.Duration(delay) * time.Second)
		suite.initSingleInstance(7, genesisTime, rng, path, "g", rolacle, poetClient)
		suite.apps[len(suite.apps)-1].startServices()
	}()
	*/
	defer suite.gracefulShutdown()

	_ = suite.apps[0].P2P.Broadcast(miner.IncomingTxProtocol, txbytes)
	timeout := time.After(6 * 60 * time.Second)

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
				if 10 <= app.state.GetBalance(dst) &&
					uint32(suite.apps[idx].mesh.LatestLayer()) == numberOfEpochs*uint32(suite.apps[idx].Config.LayersPerEpoch) { // make sure all had 1 non-genesis layer
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
			time.Sleep(10 * time.Second)
		}
	}

	suite.validateBlocksAndATXs(types.LayerID(numberOfEpochs*suite.apps[0].Config.LayersPerEpoch)-1, types.LayerID(startInLayer))

}

func (suite *AppTestSuite) validateBlocksAndATXs(untilLayer types.LayerID, startInLayer types.LayerID) {
	log.Info("untilLayer=%v", untilLayer)

	type nodeData struct {
		layertoblocks map[types.LayerID][]types.BlockID
		atxPerEpoch   map[types.EpochId]uint32
	}

	layersPerEpoch := int(suite.apps[0].Config.LayersPerEpoch)
	datamap := make(map[string]*nodeData)

	// assert all nodes validated untilLayer-1
	for _, ap := range suite.apps {
		curNodeLastLayer := ap.blockListener.ValidatedLayer()
		assert.True(suite.T(), int(untilLayer)-1 <= int(curNodeLastLayer))
	}

	for _, ap := range suite.apps {
		if _, ok := datamap[ap.nodeId.Key]; !ok {
			datamap[ap.nodeId.Key] = new(nodeData)
			datamap[ap.nodeId.Key].atxPerEpoch = make(map[types.EpochId]uint32)
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
		}
	}

	lateNodeKey := suite.apps[len(suite.apps)-1].nodeId.Key
	for i, d := range datamap {
		log.Info("Node %v in len(layerstoblocks) %v", i, len(d.layertoblocks))
		if i == lateNodeKey { // skip late node
			continue
		}
		for i2, d2 := range datamap {
			if i == i2 {
				continue
			}
			if i2 == lateNodeKey { // skip late node
				continue
			}

			assert.Equal(suite.T(), len(d.layertoblocks), len(d2.layertoblocks), "%v has not matching layer to %v. %v not %v", i, i2, len(d.layertoblocks), len(d2.layertoblocks))

			for l, bl := range d.layertoblocks {
				assert.Equal(suite.T(), len(bl), len(d2.layertoblocks[l]),
					fmt.Sprintf("%v and %v had different block maps for layer: %v: %v: %v \r\n %v: %v", i, i2, l, i, bl, i2, d2.layertoblocks[l]))
			}

			for e, atx := range d.atxPerEpoch {
				assert.Equal(suite.T(), atx, d2.atxPerEpoch[e],
					fmt.Sprintf("%v and %v had different atx counts for epoch: %v: %v: %v \r\n %v: %v", i, i2, e, i, atx, i2, d2.atxPerEpoch[e]))
			}
		}
	}

	// assuming all nodes have the same results
	layerAvgSize := suite.apps[0].Config.LayerAvgSize
	patient := datamap[suite.apps[0].nodeId.Key]

	lastLayer := len(patient.layertoblocks)

	totalBlocks := 0
	for _, l := range patient.layertoblocks {
		totalBlocks += len(l)
	}

	firstEpochBlocks := 0
	for i := 0; i < layersPerEpoch; i++ {
		if l, ok := patient.layertoblocks[types.LayerID(i)]; ok {
			firstEpochBlocks += len(l)
		}
	}

	// assert number of blocks
	totalEpochs := int(untilLayer.GetEpoch(uint16(layersPerEpoch))) + 1
	allMiners := len(suite.apps)
	exp := (layerAvgSize * layersPerEpoch) / allMiners * allMiners * (totalEpochs - 1)
	act := totalBlocks - firstEpochBlocks
	assert.Equal(suite.T(), exp, act,
		fmt.Sprintf("not good num of blocks got: %v, want: %v. totalBlocks: %v, firstEpochBlocks: %v, lastLayer: %v, layersPerEpoch: %v layerAvgSize: %v totalEpochs: %v",
			act, exp, totalBlocks, firstEpochBlocks, lastLayer, layersPerEpoch, layerAvgSize, totalEpochs))

	firstAp := suite.apps[0]
	atxDb := firstAp.blockListener.AtxDB.(*activation.ActivationDb)
	atxId, err := atxDb.GetNodeLastAtxId(firstAp.nodeId)
	assert.NoError(suite.T(), err)
	atx, err := atxDb.GetAtx(atxId)
	assert.NoError(suite.T(), err)

	totalAtxs := uint32(0)
	for atx != nil {
		totalAtxs += atx.ActiveSetSize
		atx, err = atxDb.GetAtx(atx.PrevATXId)
	}

	// assert number of ATXs
	exp = totalEpochs*allMiners - len(suite.apps) // minus last epoch #atxs
	act = int(totalAtxs)
	assert.Equal(suite.T(), exp, act, fmt.Sprintf("not good num of atxs got: %v, want: %v", act, exp))
}

func (suite *AppTestSuite) validateLastATXActiveSetSize(app *SpacemeshApp) {
	prevAtxId, err := app.atxBuilder.GetPrevAtxId(app.nodeId)
	suite.NoError(err)
	atx, err := app.mesh.GetAtx(prevAtxId)
	suite.NoError(err)
	suite.True(int(atx.ActiveSetSize) == len(suite.apps), "atx: %v node: %v", atx.ShortId(), app.nodeId.Key[:5])
}

func (suite *AppTestSuite) gracefulShutdown() {
	log.Info("Graceful shutdown begin")

	var wg sync.WaitGroup
	for _, app := range suite.apps {
		wg.Add(1)
		go func(app *SpacemeshApp) {
			app.stopServices()
			wg.Done()
		}(app)
	}
	wg.Wait()

	log.Info("Graceful shutdown end")
}

func TestAppTestSuite(t *testing.T) {
	//defer leaktest.Check(t)()
	suite.Run(t, new(AppTestSuite))
}
