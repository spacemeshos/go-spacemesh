// +build !exclude_app_test

package node

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/spacemeshos/amcl"
	"github.com/spacemeshos/amcl/BLS381"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/spacemeshos/go-spacemesh/activation"
	apicfg "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

type AppTestSuite struct {
	suite.Suite

	apps        []*SpacemeshApp
	dbs         []string
	poetCleanup func(cleanup bool) error
}

func (suite *AppTestSuite) SetupTest() {
	suite.apps = make([]*SpacemeshApp, 0, 0)
	suite.dbs = make([]string, 0, 0)
}

func (suite *AppTestSuite) TearDownTest() {
	if err := suite.poetCleanup(true); err != nil {
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
	// poet should clean up after himself
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

func Test_PoETHarnessSanity(t *testing.T) {
	h, err := activation.NewHTTPPoetHarness(true)
	require.NoError(t, err)
	require.NotNil(t, h)
}

func (suite *AppTestSuite) initMultipleInstances(cfg *config.Config, rolacle *eligibility.FixedRolacle, rng *amcl.RAND, numOfInstances int, storeFormat string, genesisTime string, poetClient *activation.HTTPPoetClient, fastHare bool, clock TickProvider, network network) {
	name := 'a'
	for i := 0; i < numOfInstances; i++ {
		dbStorepath := storeFormat + string(name)
		database.SwitchCreationContext(dbStorepath, string(name))
		smApp, err := InitSingleInstance(*cfg, i, genesisTime, rng, dbStorepath, rolacle, poetClient, clock, network)
		assert.NoError(suite.T(), err)
		suite.apps = append(suite.apps, smApp)
		suite.dbs = append(suite.dbs, dbStorepath)
		name++
	}
}

var tests = []TestScenario{txWithRunningNonceGenerator([]int{}), sameRootTester([]int{0}), reachedEpochTester([]int{}), txWithUnorderedNonceGenerator([]int{1})}

func (suite *AppTestSuite) TestMultipleNodes() {
	// suite.T().Skip()
	// EntryPointCreated <- true
	net := service.NewSimulator()
	const (
		numberOfEpochs = 5 // first 2 epochs are genesis
		numOfInstances = 5
	)
	// addr := address.BytesToAddress([]byte{0x01})
	cfg := getTestDefaultConfig(numOfInstances)
	types.SetLayersPerEpoch(int32(cfg.LayersPerEpoch))
	path := "../tmp/test/state_" + time.Now().String()

	genesisTime := time.Now().Add(20 * time.Second).Format(time.RFC3339)
	poetHarness, err := activation.NewHTTPPoetHarness(false)
	if err != nil {
		log.Panic("failed creating poet client harness: %v", err)
	}
	suite.poetCleanup = poetHarness.Teardown

	rolacle := eligibility.New()
	rng := BLS381.DefaultSeed()

	gTime, err := time.Parse(time.RFC3339, genesisTime)
	if err != nil {
		log.Error("cannot parse genesis time %v", err)
	}
	ld := time.Duration(20) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, gTime, log.NewDefault("clock"))
	suite.initMultipleInstances(cfg, rolacle, rng, numOfInstances, path, genesisTime, poetHarness.HTTPPoetClient, false, clock, net)
	for _, a := range suite.apps {
		a.startServices()
	}

	ActivateGrpcServer(suite.apps[0])

	if err := poetHarness.Start([]string{"127.0.0.1:9094"}); err != nil {
		log.Panic("failed to start poet server: %v", err)
	}

	// defer GracefulShutdown(suite.apps)

	timeout := time.After(6 * 60 * time.Second)
	setupTests(suite)
	finished := map[int]bool{}
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			GracefulShutdown(suite.apps)
			suite.T().Fatal("timed out")
		default:
			if runTests(suite, finished) {
				break loop
			}
			time.Sleep(10 * time.Second)
		}
	}
	suite.validateBlocksAndATXs(types.LayerID(numberOfEpochs*suite.apps[0].Config.LayersPerEpoch) - 1)
	oldRoot := suite.apps[0].state.GetStateRoot()
	GracefulShutdown(suite.apps)

	// this tests loading of previous state, maybe it's not the best place to put this here...
	smApp, err := InitSingleInstance(*cfg, 0, genesisTime, rng, path+"a", rolacle, poetHarness.HTTPPoetClient, clock, net)
	assert.NoError(suite.T(), err)
	// test that loaded root equals
	assert.Equal(suite.T(), oldRoot, smApp.state.GetStateRoot())
	// start and stop and test for no panics
	smApp.startServices()
	smApp.stopServices()
}

type ScenarioSetup func(suit *AppTestSuite, t *testing.T)

type ScenarioTestCriteria func(suit *AppTestSuite, t *testing.T) bool

type TestScenario struct {
	Setup        ScenarioSetup
	Criteria     ScenarioTestCriteria
	Dependencies []int
}

func txWithUnorderedNonceGenerator(dependencies []int) TestScenario {

	acc1Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(apicfg.Account2Private))
	addr := types.Address{}
	addr.SetBytes(acc1Signer.PublicKey().Bytes())
	dst := types.BytesToAddress([]byte{0x09})
	txsSent := 25
	setup := func(suite *AppTestSuite, t *testing.T) {
		if err != nil {
			log.Panic("Could not build ed signer err=%v", err)
		}

		for i := 0; i < txsSent; i++ {
			tx, err := types.NewSignedTx(uint64(txsSent-i), dst, 10, 1, 1, acc1Signer)
			if err != nil {
				log.Panic("panicked creating signed tx err=%v", err)
			}
			txbytes, _ := types.InterfaceToBytes(tx)
			pbMsg := pb.SignedTransaction{Tx: txbytes}
			_, err = suite.apps[0].grpcAPIService.SubmitTransaction(nil, &pbMsg)
			assert.Error(suite.T(), err)
		}
	}

	teardown := func(suite *AppTestSuite, t *testing.T) bool {
		ok := true
		for _, app := range suite.apps {
			log.Info("zero acc current balance: %d nonce %d", app.state.GetBalance(dst), app.state.GetNonce(addr))
			ok = ok && 0 == app.state.GetBalance(dst) && app.state.GetNonce(addr) == 0
		}
		if ok {
			log.Info("zero addresses ok")
		}
		return ok
	}

	return TestScenario{setup, teardown, dependencies}
}

func txWithRunningNonceGenerator(dependencies []int) TestScenario {

	acc1Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(apicfg.Account1Private))
	addr := types.Address{}
	addr.SetBytes(acc1Signer.PublicKey().Bytes())
	dst := types.BytesToAddress([]byte{0x02})
	txsSent := 25
	setup := func(suite *AppTestSuite, t *testing.T) {
		if err != nil {
			log.Panic("Could not build ed signer err=%v", err)
		}

		account := pb.AccountId{}
		account.Address = addr.String()

		for i := 0; i < txsSent; i++ {

			nonceStr, err := suite.apps[0].grpcAPIService.GetNonce(nil, &account)
			assert.NoError(suite.T(), err)
			actNonce, err := strconv.Atoi(nonceStr.Value)
			for actNonce != i {
				log.Info("actual nonce: %v", nonceStr.Value)
				time.Sleep(500 * time.Millisecond)
				nonceStr, err = suite.apps[0].grpcAPIService.GetNonce(nil, &account)
				assert.NoError(suite.T(), err)
				actNonce, err = strconv.Atoi(nonceStr.Value)
			}
			tx, err := types.NewSignedTx(uint64(i), dst, 10, 1, 1, acc1Signer)
			if err != nil {
				log.Panic("panicked creating signed tx err=%v", err)
			}
			txbytes, _ := types.InterfaceToBytes(tx)
			pbMsg := pb.SignedTransaction{Tx: txbytes}
			// _ = suite.apps[0].P2P.Broadcast(miner.IncomingTxProtocol, txbytes)
			_, err = suite.apps[0].grpcAPIService.SubmitTransaction(nil, &pbMsg)
			assert.NoError(suite.T(), err)
		}
	}

	teardown := func(suite *AppTestSuite, t *testing.T) bool {
		ok := true
		for _, app := range suite.apps {
			log.Info("current balance: %d nonce %d", app.state.GetBalance(dst), app.state.GetNonce(addr))
			ok = ok && 250 <= app.state.GetBalance(dst) && app.state.GetNonce(addr) == uint64(txsSent)
		}
		if ok {
			log.Info("addresses ok")
		}
		return ok
	}

	return TestScenario{setup, teardown, dependencies}
}

func reachedEpochTester(dependencies []int) TestScenario {
	const numberOfEpochs = 5 // first 2 epochs are genesis
	setup := func(suite *AppTestSuite, t *testing.T) {}

	test := func(suite *AppTestSuite, t *testing.T) bool {
		expectedTotalWeight := configuredTotalWeight(suite.apps)
		for _, app := range suite.apps {
			if uint32(app.mesh.LatestLayer()) < numberOfEpochs*uint32(app.Config.LayersPerEpoch) {
				return false
			}
			suite.validateLastATXTotalWeight(app, numberOfEpochs, expectedTotalWeight)
		}
		log.Info("epoch ok")
		return true
	}
	return TestScenario{setup, test, dependencies}
}

func configuredTotalWeight(apps []*SpacemeshApp) uint64 {
	expectedTotalWeight := uint64(0)
	for _, app := range apps {
		expectedTotalWeight += app.Config.SpaceToCommit
	}
	return expectedTotalWeight
}

func sameRootTester(dependencies []int) TestScenario {
	setup := func(suite *AppTestSuite, t *testing.T) {}

	test := func(suite *AppTestSuite, t *testing.T) bool {
		stickyClientsDone := 0
		maxClientsDone := 0
		for idx, app := range suite.apps {
			clientsDone := 0
			for idx2, app2 := range suite.apps {
				if idx != idx2 {
					r1 := app.state.IntermediateRoot(false).String()
					r2 := app2.state.IntermediateRoot(false).String()
					if r1 == r2 {
						clientsDone++
						if clientsDone == len(suite.apps)-1 {
							log.Info("%d roots confirmed out of %d return ok", clientsDone, len(suite.apps))
							return true
						}
					}
				}
			}
			if clientsDone > maxClientsDone {
				maxClientsDone = clientsDone
			}
		}

		if maxClientsDone != stickyClientsDone {
			stickyClientsDone = maxClientsDone
			log.Info("%d roots confirmed out of %d", maxClientsDone, len(suite.apps))
		}
		return false
	}

	return TestScenario{setup, test, dependencies}
}

// run setup on all tests
func setupTests(suite *AppTestSuite) {
	for _, test := range tests {
		test.Setup(suite, suite.T())
	}
}

// run test criterias after setup
func runTests(suite *AppTestSuite, finished map[int]bool) bool {
	for i, test := range tests {
		depsOk := true
		for _, x := range test.Dependencies {
			done, has := finished[x]
			depsOk = depsOk && done && has
		}
		if depsOk && !finished[i] {
			finished[i] = test.Criteria(suite, suite.T())
			log.Info("test %d has finished with result %v", i, finished[i])
		}
	}
	for i := 0; i < len(tests); i++ {
		testOk, hasTest := finished[i]
		if hasTest {
			if !testOk {
				return false
			}
		} else {
			// test didnt run yet
			return false
		}
	}
	return true
}

func (suite *AppTestSuite) validateBlocksAndATXs(untilLayer types.LayerID) {
	log.Info("untilLayer=%v", untilLayer)

	type nodeData struct {
		layertoblocks map[types.LayerID][]types.BlockID
		atxPerEpoch   map[types.EpochID]uint32
	}

	layersPerEpoch := suite.apps[0].Config.LayersPerEpoch
	datamap := make(map[string]*nodeData)

	// assert all nodes validated untilLayer-1
	for _, ap := range suite.apps {
		curNodeLastLayer := ap.blockListener.ProcessedLayer()
		assert.True(suite.T(), int(untilLayer)-1 <= int(curNodeLastLayer))
	}

	for _, ap := range suite.apps {
		if _, ok := datamap[ap.nodeID.Key]; !ok {
			datamap[ap.nodeID.Key] = new(nodeData)
			datamap[ap.nodeID.Key].atxPerEpoch = make(map[types.EpochID]uint32)
			datamap[ap.nodeID.Key].layertoblocks = make(map[types.LayerID][]types.BlockID)
		}

		for i := types.LayerID(5); i <= untilLayer; i++ {
			lyr, err := ap.blockListener.GetLayer(i)
			if err != nil {
				log.Error("ERROR: couldn't get a validated layer from db layer %v, %v", i, err)
			}
			for _, b := range lyr.Blocks() {
				datamap[ap.nodeID.Key].layertoblocks[lyr.Index()] = append(datamap[ap.nodeID.Key].layertoblocks[lyr.Index()], b.ID())
			}
		}
	}

	lateNodeKey := suite.apps[len(suite.apps)-1].nodeID.Key
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
	patient := datamap[suite.apps[0].nodeID.Key]

	lastLayer := len(patient.layertoblocks) + 5
	log.Info("patient %v", suite.apps[0].nodeID.ShortString())

	totalBlocks := 0
	for id, l := range patient.layertoblocks {
		totalBlocks += len(l)
		log.Info("patient %v layer %v, blocks %v", suite.apps[0].nodeID.String(), id, len(l))
	}

	genesisBlocks := 0
	for i := 0; i < layersPerEpoch*2; i++ {
		if l, ok := patient.layertoblocks[types.LayerID(i)]; ok {
			genesisBlocks += len(l)
		}
	}

	// assert number of blocks
	totalEpochs := int(untilLayer.GetEpoch()) + 1
	expectedEpochWeight := configuredTotalWeight(suite.apps)
	blocksPerEpochTarget := layerAvgSize * layersPerEpoch
	expectedBlocksPerEpoch := 0
	for _, app := range suite.apps {
		expectedBlocksPerEpoch += max(blocksPerEpochTarget*int(app.Config.SpaceToCommit)/int(expectedEpochWeight), 1)
	}

	expectedTotalBlocks := (totalEpochs - 2) * expectedBlocksPerEpoch
	actualTotalBlocks := totalBlocks - genesisBlocks
	assert.Equal(suite.T(), expectedTotalBlocks, actualTotalBlocks,
		fmt.Sprintf("not good num of blocks got: %v, want: %v. totalBlocks: %v, genesisBlocks: %v, lastLayer: %v, layersPerEpoch: %v layerAvgSize: %v totalEpochs: %v datamap: %v",
			actualTotalBlocks, expectedTotalBlocks, totalBlocks, genesisBlocks, lastLayer, layersPerEpoch, layerAvgSize, totalEpochs, datamap))

	totalWeightAllEpochs := calcTotalWeight(assert.New(suite.T()), suite.apps)

	// assert number of ATXs
	allMiners := len(suite.apps)
	expectedTotalWeight := uint64(totalEpochs) * expectedEpochWeight
	assert.Equal(suite.T(), expectedTotalWeight, totalWeightAllEpochs, fmt.Sprintf("not good total atx weight. got: %v, want: %v.\ntotalEpochs: %v, allMiners: %v", totalWeightAllEpochs, expectedTotalWeight, totalEpochs, allMiners))
}

func max(i, j int) int {
	if i > j {
		return i
	}
	return j
}

func calcTotalWeight(assert *assert.Assertions, apps []*SpacemeshApp) (totalWeightAllEpochs uint64) {
	app := apps[0]
	atxDb := app.blockListener.AtxDB.(*activation.DB)

	for _, app := range apps {
		atxID, err := atxDb.GetNodeLastAtxID(app.nodeID)
		assert.NoError(err)

		for atxID != *types.EmptyATXID {
			atx, err := atxDb.GetAtxHeader(atxID)
			assert.NoError(err)
			totalWeightAllEpochs += atx.GetWeight()
			log.Info("added atx. pub layer: %v, target epoch: %v, weight: %v",
				atx.PubLayerID, atx.TargetEpoch(), atx.GetWeight())
			atxID = atx.PrevATXID
		}
	}
	return totalWeightAllEpochs
}

func (suite *AppTestSuite) validateLastATXTotalWeight(app *SpacemeshApp, numberOfEpochs int, expectedTotalWeight uint64) {
	atxs := app.atxDb.GetEpochAtxs(types.EpochID(numberOfEpochs - 1))
	suite.Len(atxs, len(suite.apps), "node: %v", app.nodeID.ShortString())

	totalWeight := uint64(0)
	for _, atxID := range atxs {
		atx, _ := app.atxDb.GetAtxHeader(atxID)
		totalWeight += atx.GetWeight()
	}
	suite.Equal(expectedTotalWeight, totalWeight, "node: %v", app.nodeID.ShortString())
}

// CI has a 10 minute timeout
// this ensures we print something before the timeout
func patchCITimeout(termchan chan struct{}) {
	ticker := time.NewTimer(5 * time.Minute)
	for {
		select {
		case <-ticker.C:
			fmt.Printf("CI timeout patch\n")
			ticker = time.NewTimer(5 * time.Minute)
		case <-termchan:
			return
		}
	}
}

func TestAppTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	// defer leaktest.Check(t)()
	term := make(chan struct{})
	go patchCITimeout(term)
	suite.Run(t, new(AppTestSuite))
	close(term)
}

func TestShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// make sure previous goroutines has stopped
	time.Sleep(3 * time.Second)
	gCount := runtime.NumGoroutine()
	net := service.NewSimulator()
	// defer leaktest.Check(t)()
	r := require.New(t)

	smApp := NewSpacemeshApp()
	genesisTime := time.Now().Add(time.Second * 10)
	smApp.Config.POST = activation.DefaultConfig()
	smApp.Config.POST.Difficulty = 5
	smApp.Config.POST.NumProvenLabels = 10
	smApp.Config.POST.SpacePerUnit = 1 << 10 // 1KB.
	smApp.Config.POST.NumFiles = 1

	smApp.Config.HARE.N = 5
	smApp.Config.HARE.F = 2
	smApp.Config.HARE.RoundDuration = 3
	smApp.Config.HARE.WakeupDelta = 5
	smApp.Config.HARE.ExpectedLeaders = 5
	smApp.Config.CoinbaseAccount = "0x123"
	smApp.Config.LayerAvgSize = 5
	smApp.Config.LayersPerEpoch = 3
	smApp.Config.TxsPerBlock = 100
	smApp.Config.Hdist = 5
	smApp.Config.GenesisTime = genesisTime.Format(time.RFC3339)
	smApp.Config.LayerDurationSec = 20
	smApp.Config.HareEligibility.ConfidenceParam = 3
	smApp.Config.HareEligibility.EpochOffset = 0
	smApp.Config.StartMining = true

	rolacle := eligibility.New()
	types.SetLayersPerEpoch(int32(smApp.Config.LayersPerEpoch))

	edSgn := signing.NewEdSigner()
	pub := edSgn.PublicKey()

	poetHarness, err := activation.NewHTTPPoetHarness(false)
	if err != nil {
		log.Panic("failed creating poet harness: %v", err)
	}

	vrfPriv, vrfPub := BLS381.GenKeyPair(BLS381.DefaultSeed())
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)
	nodeID := types.NodeID{Key: pub.String(), VRFPublicKey: vrfPub}

	swarm := net.NewNode()
	dbStorepath := "/tmp/" + pub.String()

	hareOracle := newLocalOracle(rolacle, 5, nodeID)
	hareOracle.Register(true, pub.String())

	postClient, err := activation.NewPostClient(&smApp.Config.POST, util.Hex2Bytes(nodeID.Key))
	r.NoError(err)
	r.NotNil(postClient)
	gTime := genesisTime
	ld := time.Duration(20) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, gTime, log.NewDefault("clock"))
	err = smApp.initServices(nodeID, swarm, dbStorepath, edSgn, false, hareOracle, uint32(smApp.Config.LayerAvgSize), postClient, poetHarness.HTTPPoetClient, vrfSigner, uint16(smApp.Config.LayersPerEpoch), clock)

	r.NoError(err)

	smApp.startServices()
	ActivateGrpcServer(smApp)

	poetHarness.Teardown(true)
	smApp.stopServices()

	time.Sleep(5 * time.Second)
	gCount2 := runtime.NumGoroutine()

	if gCount != gCount2 {
		buf := make([]byte, 4096)
		runtime.Stack(buf, true)
		log.Error(string(buf))
	}
	require.Equal(t, gCount, gCount2)
}
