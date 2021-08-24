// +build !exclude_app_test

package node

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/spacemeshos/go-spacemesh/activation"
	apicfg "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon"
)

type AppTestSuite struct {
	suite.Suite
	log         log.Log
	apps        []*App
	poetCleanup func(cleanup bool) error
}

func (suite *AppTestSuite) SetupTest() {
	suite.apps = make([]*App, 0, 0)
	suite.log = logtest.New(suite.T())
	suite.poetCleanup = func(bool) error { return nil }
}

func (suite *AppTestSuite) TearDownTest() {
	// poet should clean up after itself
	if matches, err := filepath.Glob("*.bin"); err != nil {
		suite.log.With().Error("error while finding poet bin files", log.Err(err))
	} else {
		for _, f := range matches {
			if err = os.Remove(f); err != nil {
				suite.log.With().Error("error while cleaning up poet bin files", log.Err(err))
			}
		}
	}
}

func Test_PoETHarnessSanity(t *testing.T) {
	h, err := activation.NewHTTPPoetHarness(true)
	require.NoError(t, err)
	require.NotNil(t, h)
}

func (suite *AppTestSuite) initMultipleInstances(cfg *config.Config, rolacle *eligibility.FixedRolacle, numOfInstances int, storeFormat string, genesisTime string, poetClient *activation.HTTPPoetClient, clock TickProvider, network network) {
	name := 'a'
	for i := 0; i < numOfInstances; i++ {
		dbStorepath := storeFormat + string(name)
		database.SwitchCreationContext(dbStorepath, string(name))
		edSgn := signing.NewEdSigner()
		smApp, err := InitSingleInstance(logtest.New(suite.T()), *cfg, i, genesisTime, dbStorepath, rolacle, poetClient, clock, network, edSgn)
		suite.NoError(err)
		suite.apps = append(suite.apps, smApp)
		name++
	}
}

func (suite *AppTestSuite) ClosePoet() {
	if err := suite.poetCleanup(true); err != nil {
		suite.log.With().Error("error while cleaning up poet", log.Err(err))
	}
}

var tests = []TestScenario{
	txWithRunningNonceGenerator([]int{}),
	sameRootTester([]int{0}),
	reachedEpochTester([]int{}),
	txWithUnorderedNonceGenerator([]int{1}),
}

func (suite *AppTestSuite) TestMultipleNodes() {
	net := service.NewSimulator()
	const (
		numberOfEpochs = 5 // first 2 epochs are genesis
		numOfInstances = 5
	)
	cfg := getTestDefaultConfig(numOfInstances)
	types.SetLayersPerEpoch(cfg.LayersPerEpoch)
	path := suite.T().TempDir()
	lg := logtest.New(suite.T())

	genesisTime := time.Now().Add(20 * time.Second).Format(time.RFC3339)
	poetHarness, err := activation.NewHTTPPoetHarness(false)
	require.NoError(suite.T(), err, "failed creating poet client harness: %v", err)
	suite.poetCleanup = poetHarness.Teardown

	// Scan and print the poet output, and catch errors early
	failChan := make(chan struct{})
	go func() {
		poetLog := lg.WithName("poet")
		scanner := bufio.NewScanner(io.MultiReader(poetHarness.Stdout, poetHarness.Stderr))
		for scanner.Scan() {
			line := scanner.Text()
			matched, err := regexp.MatchString(`\bERROR\b`, line)
			require.NoError(suite.T(), err)
			// Fail fast if we encounter a poet error
			// Must use a channel since we're running inside a goroutine
			if matched {
				close(failChan)
				suite.T().Fatalf("got error from poet: %s", line)
			}
			poetLog.Debug(line)
		}
	}()

	rolacle := eligibility.New(lg)

	gTime, err := time.Parse(time.RFC3339, genesisTime)
	if err != nil {
		suite.log.With().Error("cannot parse genesis time", log.Err(err))
	}
	ld := time.Duration(20) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, gTime, logtest.New(suite.T()))
	suite.initMultipleInstances(cfg, rolacle, numOfInstances, path, genesisTime, poetHarness.HTTPPoetClient, clock, net)

	// We must shut down before running the rest of the tests or we'll get an error about resource unavailable
	// when we try to allocate more database files. Wrap this context neatly in an inline func.
	var oldRoot types.Hash32
	var edSgn *signing.EdSigner
	func() {
		defer GracefulShutdown(suite.apps)
		defer suite.ClosePoet()

		for _, a := range suite.apps {
			a.startServices(context.TODO(), logtest.New(suite.T()))
		}

		ActivateGrpcServer(suite.apps[0])

		if err := poetHarness.Start(context.TODO(), []string{"127.0.0.1:9094"}); err != nil {
			suite.T().Fatalf("failed to start poet server: %v", err)
		}

		timeout := time.After(6 * time.Minute)

		// Run setup first. We need to allow this to timeout, and monitor the failure channel too,
		// as this can also loop forever.
		doneChan := make(chan struct{})
		go func() {
			setupTests(suite)
			close(doneChan)
		}()

	loopSetup:
		for {
			select {
			case <-doneChan:
				break loopSetup
			case <-timeout:
				suite.T().Fatal("timed out")
			case <-failChan:
				suite.T().Fatal("error from poet harness")
			}
		}

		finished := map[int]bool{}
	loop:
		for {
			select {
			case <-timeout:
				suite.T().Fatal("timed out")
			case <-failChan:
				suite.T().Fatal("error from poet harness")
			default:
				if runTests(suite, finished) {
					break loop
				}
				time.Sleep(20 * time.Second)
			}
		}
		suite.validateBlocksAndATXs(types.NewLayerID(numberOfEpochs * suite.apps[0].Config.LayersPerEpoch).Sub(1))
		oldRoot = suite.apps[0].state.GetStateRoot()
		edSgn = suite.apps[0].edSgn
	}()

	// this tests loading of previous state, maybe it's not the best place to put this here...
	smApp, err := InitSingleInstance(lg, *cfg, 0, genesisTime, path+"a", rolacle, poetHarness.HTTPPoetClient, clock, net, edSgn)
	assert.NoError(suite.T(), err)
	// test that loaded root is equal
	assert.Equal(suite.T(), oldRoot, smApp.state.GetStateRoot())
	// start and stop and test for no panics
	smApp.startServices(context.TODO(), logtest.New(suite.T()))
	smApp.stopServices()
}

type (
	ScenarioSetup        func(*AppTestSuite, *testing.T)
	ScenarioTestCriteria func(*AppTestSuite, *testing.T) bool
	TestScenario         struct {
		Setup        ScenarioSetup
		Criteria     ScenarioTestCriteria
		Dependencies []int
	}
)

func txWithUnorderedNonceGenerator(dependencies []int) TestScenario {
	acc1Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(apicfg.Account2Private))
	if err != nil {
		log.Panic("could not build ed signer", log.Err(err))
	}
	addr := types.Address{}
	addr.SetBytes(acc1Signer.PublicKey().Bytes())
	dst := types.BytesToAddress([]byte{0x09})
	txsSent := 25
	setup := func(suite *AppTestSuite, t *testing.T) {
		for i := 0; i < txsSent; i++ {
			tx, err := types.NewSignedTx(uint64(txsSent-i), dst, 10, 1, 1, acc1Signer)
			if err != nil {
				log.Panic("panicked creating signed tx", log.Err(err))
			}
			txbytes, _ := types.InterfaceToBytes(tx)
			pbMsg := &pb.SubmitTransactionRequest{Transaction: txbytes}
			_, err = suite.apps[0].txService.SubmitTransaction(nil, pbMsg)
			assert.Error(suite.T(), err)
		}
	}

	teardown := func(suite *AppTestSuite, t *testing.T) bool {
		ok := true
		for _, app := range suite.apps {
			ok = ok && 0 == app.state.GetBalance(dst) && app.state.GetNonce(addr) == 0
		}
		return ok
	}

	return TestScenario{setup, teardown, dependencies}
}

func txWithRunningNonceGenerator(dependencies []int) TestScenario {
	acc1Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(apicfg.Account1Private))
	if err != nil {
		log.With().Panic("could not build ed signer", log.Err(err))
	}

	addr := types.Address{}
	addr.SetBytes(acc1Signer.PublicKey().Bytes())
	dst := types.BytesToAddress([]byte{0x02})
	txsSent := 25
	setup := func(suite *AppTestSuite, t *testing.T) {
		accountRequest := &pb.AccountRequest{AccountId: &pb.AccountId{Address: addr.Bytes()}}
		getNonce := func() int {
			accountResponse, err := suite.apps[0].globalstateSvc.Account(nil, accountRequest)
			assert.NoError(suite.T(), err)
			// Check the projected state. We just want to know that the tx has entered
			// the mempool successfully.
			return int(accountResponse.AccountWrapper.StateProjected.Counter)
		}

		for i := 0; i < txsSent; i++ {
			actNonce := getNonce()

			// Note: this may loop forever if the nonce is not advancing for some reason, but the entire
			// setup process will timeout above if this happens
			for i != actNonce {
				time.Sleep(250 * time.Millisecond)
				actNonce = getNonce()
			}
			tx, err := types.NewSignedTx(uint64(i), dst, 10, 1, 1, acc1Signer)
			suite.NoError(err, "failed to create signed tx: %s", err)
			txbytes, _ := types.InterfaceToBytes(tx)
			pbMsg := &pb.SubmitTransactionRequest{Transaction: txbytes}
			_, err = suite.apps[0].txService.SubmitTransaction(nil, pbMsg)
			suite.NoError(err, "error submitting transaction")
		}
	}

	teardown := func(suite *AppTestSuite, t *testing.T) bool {
		ok := true
		for _, app := range suite.apps {
			ok = ok && 250 <= app.state.GetBalance(dst) && app.state.GetNonce(addr) == uint64(txsSent)
		}
		return ok
	}

	return TestScenario{setup, teardown, dependencies}
}

func reachedEpochTester(dependencies []int) TestScenario {
	const numberOfEpochs = 5 // first 2 epochs are genesis
	setup := func(*AppTestSuite, *testing.T) {}

	test := func(suite *AppTestSuite, t *testing.T) bool {
		expectedTotalWeight := configuredTotalWeight(suite.apps)
		for _, app := range suite.apps {
			if app.mesh.LatestLayer().Before(types.NewLayerID(numberOfEpochs * uint32(app.Config.LayersPerEpoch))) {
				return false
			}
			suite.validateLastATXTotalWeight(app, numberOfEpochs, expectedTotalWeight)
		}
		// weakcoin test runs once, after epoch has been reached
		suite.healingWeakcoinTester()
		return true
	}
	return TestScenario{setup, test, dependencies}
}

// test that all nodes see the same weak coin value in each layer
func (suite *AppTestSuite) healingWeakcoinTester() {
	globalLayer := suite.apps[0].mesh.LatestLayer()
	globalCoin := make(map[types.LayerID]bool)
	for i, app := range suite.apps {
		lastLayer := app.mesh.LatestLayer()
		suite.Equal(globalLayer, lastLayer, "bad last layer on node %v", i)
		// there will be no coin value for layer zero since ticker delivers only new layers
		// and there will be no coin value for the last layer
		for layerID := types.NewLayerID(1); layerID.Before(lastLayer); layerID = layerID.Add(1) {
			coinflip, exists := app.mesh.DB.GetCoinflip(context.TODO(), layerID)
			if !exists {
				suite.Fail("no weak coin value", "node %v layer %v last layer %v", i, layerID, lastLayer)
				continue
			}
			if gc, layerExists := globalCoin[layerID]; layerExists {
				suite.Equal(gc, coinflip, "bad weak coin value on node %v layer %v last layer %v", i, layerID, lastLayer)
			} else {
				globalCoin[layerID] = coinflip
			}
		}
	}
}

func configuredTotalWeight(apps []*App) uint64 {
	expectedTotalWeight := uint64(0)
	for _, app := range apps {
		expectedTotalWeight += uint64(app.Config.SMESHING.Opts.NumUnits)
	}
	return expectedTotalWeight
}

func sameRootTester(dependencies []int) TestScenario {
	setup := func(*AppTestSuite, *testing.T) {}

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
		}
		if !finished[i] {
			// at least one test isn't completed, pre-empt and return to keep looping
			return false
		}
	}
	return true
}

func (suite *AppTestSuite) validateBlocksAndATXs(untilLayer types.LayerID) {
	type nodeData struct {
		layertoblocks map[types.LayerID][]types.BlockID
		atxPerEpoch   map[types.EpochID]uint32
	}

	layersPerEpoch := suite.apps[0].Config.LayersPerEpoch
	datamap := make(map[string]*nodeData)

	// assert all nodes validated untilLayer-1
	for _, ap := range suite.apps {
		curNodeLastLayer := ap.mesh.ProcessedLayer()
		assert.True(suite.T(), !untilLayer.Sub(1).After(curNodeLastLayer))
	}

	for _, ap := range suite.apps {
		if _, ok := datamap[ap.nodeID.Key]; !ok {
			datamap[ap.nodeID.Key] = new(nodeData)
			datamap[ap.nodeID.Key].atxPerEpoch = make(map[types.EpochID]uint32)
			datamap[ap.nodeID.Key].layertoblocks = make(map[types.LayerID][]types.BlockID)
		}

		for i := types.NewLayerID(5); !i.After(untilLayer); i = i.Add(1) {
			lyr, err := ap.mesh.GetLayer(i)
			suite.NoError(err, "couldn't get validated layer from db", i)
			for _, b := range lyr.Blocks() {
				datamap[ap.nodeID.Key].layertoblocks[lyr.Index()] = append(datamap[ap.nodeID.Key].layertoblocks[lyr.Index()], b.ID())
			}
		}
	}

	lateNodeKey := suite.apps[len(suite.apps)-1].nodeID.Key
	for i, d := range datamap {
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

	totalBlocks := 0
	for _, l := range patient.layertoblocks {
		totalBlocks += len(l)
	}

	genesisBlocks := 0
	for i := uint32(0); i < layersPerEpoch*2; i++ {
		if l, ok := patient.layertoblocks[types.NewLayerID(i)]; ok {
			genesisBlocks += len(l)
		}
	}

	// assert number of blocks
	totalEpochs := int(untilLayer.GetEpoch()) + 1
	expectedEpochWeight := configuredTotalWeight(suite.apps)
	blocksPerEpochTarget := uint32(layerAvgSize) * layersPerEpoch
	expectedBlocksPerEpoch := 0
	for _, app := range suite.apps {
		expectedBlocksPerEpoch += max(int(blocksPerEpochTarget)*int(app.Config.SMESHING.Opts.NumUnits)/int(expectedEpochWeight), 1)
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

func calcTotalWeight(assert *assert.Assertions, apps []*App) (totalWeightAllEpochs uint64) {
	app := apps[0]
	atxDb := app.atxDb

	for _, app := range apps {
		atxID, err := atxDb.GetNodeLastAtxID(app.nodeID)
		assert.NoError(err)

		for atxID != *types.EmptyATXID {
			atx, err := atxDb.GetAtxHeader(atxID)
			assert.NoError(err)
			totalWeightAllEpochs += atx.GetWeight()
			atxID = atx.PrevATXID
		}
	}
	return totalWeightAllEpochs
}

func (suite *AppTestSuite) validateLastATXTotalWeight(app *App, numberOfEpochs int, expectedTotalWeight uint64) {
	atxs := app.atxDb.GetEpochAtxs(types.EpochID(numberOfEpochs - 1))
	suite.Len(atxs, len(suite.apps), "node: %v", app.nodeID.ShortString())

	totalWeight := uint64(0)
	for _, atxID := range atxs {
		atx, _ := app.atxDb.GetAtxHeader(atxID)
		totalWeight += atx.GetWeight()
	}
	suite.Equal(expectedTotalWeight, totalWeight, "node: %v", app.nodeID.ShortString())
}

func TestAppTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	suite.Run(t, new(AppTestSuite))
}

func TestShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// make sure previous goroutines have stopped
	time.Sleep(3 * time.Second)
	gCount := runtime.NumGoroutine()
	net := service.NewSimulator()
	r := require.New(t)

	smApp := New(WithLog(logtest.New(t)))
	genesisTime := time.Now().Add(time.Second * 10)

	smApp.Config.POST.BitsPerLabel = 8
	smApp.Config.POST.LabelsPerUnit = 32
	smApp.Config.POST.K2 = 4

	smApp.Config.SMESHING.Start = true
	smApp.Config.SMESHING.CoinbaseAccount = "0x123"
	smApp.Config.SMESHING.Opts.DataDir = t.TempDir()
	smApp.Config.SMESHING.Opts.ComputeProviderID = int(initialization.CPUProviderID())

	smApp.Config.HARE.N = 5
	smApp.Config.HARE.F = 2
	smApp.Config.HARE.RoundDuration = 3
	smApp.Config.HARE.WakeupDelta = 5
	smApp.Config.HARE.ExpectedLeaders = 5
	smApp.Config.GoldenATXID = "0x5678"
	smApp.Config.LayerAvgSize = 5
	smApp.Config.LayersPerEpoch = 3
	smApp.Config.TxsPerBlock = 100
	smApp.Config.Hdist = 5
	smApp.Config.GenesisTime = genesisTime.Format(time.RFC3339)
	smApp.Config.LayerDurationSec = 20
	smApp.Config.HareEligibility.ConfidenceParam = 3
	smApp.Config.HareEligibility.EpochOffset = 0
	smApp.Config.TortoiseBeacon = tortoisebeacon.NodeSimUnitTestConfig()

	smApp.Config.FETCH.RequestTimeout = 1
	smApp.Config.FETCH.BatchTimeout = 1
	smApp.Config.FETCH.BatchSize = 5
	smApp.Config.FETCH.MaxRetiresForPeer = 5

	rolacle := eligibility.New(logtest.New(t))
	types.SetLayersPerEpoch(smApp.Config.LayersPerEpoch)

	edSgn := signing.NewEdSigner()
	pub := edSgn.PublicKey()

	poetHarness, err := activation.NewHTTPPoetHarness(false)
	r.NoError(err, "failed creating poet client harness: %v", err)

	vrfSigner, vrfPub, err := signing.NewVRFSigner(pub.Bytes())
	r.NoError(err, "failed to create vrf signer")
	nodeID := types.NodeID{Key: pub.String(), VRFPublicKey: vrfPub}

	swarm := net.NewNode()
	dbStorepath := t.TempDir()

	hareOracle := newLocalOracle(rolacle, 5, nodeID)
	hareOracle.Register(true, pub.String())

	gTime := genesisTime
	ld := time.Duration(20) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, gTime, logtest.New(t))
	err = smApp.initServices(context.TODO(), nodeID, swarm, dbStorepath, edSgn, false, hareOracle, uint32(smApp.Config.LayerAvgSize), poetHarness.HTTPPoetClient, vrfSigner, smApp.Config.LayersPerEpoch, clock)

	r.NoError(err)

	smApp.startServices(context.TODO(), logtest.New(t))
	ActivateGrpcServer(smApp)

	r.NoError(poetHarness.Teardown(true))
	smApp.stopServices()

	time.Sleep(5 * time.Second)
	gCount2 := runtime.NumGoroutine()

	if !assert.Equal(t, gCount, gCount2) {
		buf := make([]byte, 4096)
		runtime.Stack(buf, true)
		logtest.New(t).Error(string(buf))
	}
}
