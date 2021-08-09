// +build !exclude_app_test

package node

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
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
	apps        []*App
	killedApps  []*App
	killEpoch   types.EpochID
	log         log.Log
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

func (suite *AppTestSuite) initMultipleInstances(cfg *config.Config, rolacle *eligibility.FixedRolacle, numOfInstances int, genesisTime string, poetClient *activation.HTTPPoetClient, clock TickProvider, network network) (firstDir string) {
	name := 'a'
	for i := 0; i < numOfInstances; i++ {
		dbStorepath := suite.T().TempDir()
		if i == 0 {
			firstDir = dbStorepath
		}
		database.SwitchCreationContext(dbStorepath, string(name))
		edSgn := signing.NewEdSigner()
		smApp, err := InitSingleInstance(logtest.New(suite.T()), *cfg, i, genesisTime, dbStorepath, rolacle, poetClient, clock, network, edSgn)
		suite.NoError(err)
		suite.apps = append(suite.apps, smApp)
		name++
	}
	return
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
	healingTester([]int{}), // run last as it kills some of the running apps!
}

type sharedClock struct {
	*timesync.TimeClock
}

func (clock sharedClock) Close() {
	// wrap the Close method so closing one app doesn't close the clock for other listeners
	log.Info("simulating clock close")
}

func (clock sharedClock) RealClose() {
	// actually close the underlying clock
	clock.TimeClock.Close()
}

func (suite *AppTestSuite) TestMultipleNodes() {
	net := service.NewSimulator()
	const (
		numberOfEpochs = 5 // first 2 epochs are genesis
		numOfInstances = 5
		//numOfInstances = 10
	)
	cfg := getTestDefaultConfig(numOfInstances)
	types.SetLayersPerEpoch(cfg.LayersPerEpoch)
	lg := logtest.New(suite.T())

	genesisTime := time.Now().Add(20 * time.Second).Format(time.RFC3339)
	poetHarness, err := activation.NewHTTPPoetHarness(false)
	suite.NoError(err, "failed creating poet client harness: %v", err)
	suite.poetCleanup = poetHarness.Teardown

	// Scan and print the poet output, and catch errors early
	failChan := make(chan struct{})
	go func() {
		poetLog := lg.WithName("poet")
		scanner := bufio.NewScanner(io.MultiReader(poetHarness.Stdout, poetHarness.Stderr))
		for scanner.Scan() {
			line := scanner.Text()
			matched, err := regexp.MatchString(`\bERROR\b`, line)
			suite.NoError(err)
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
	ld := 20 * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, gTime, logtest.New(suite.T()))
	firstDir := suite.initMultipleInstances(cfg, rolacle, numOfInstances, genesisTime, poetHarness.HTTPPoetClient, clock, net)

	// We must shut down before running the rest of the tests or we'll get an error about resource unavailable
	// when we try to allocate more database files. Wrap this context neatly in an inline func.
	var oldRoot types.Hash32
	var edSgn *signing.EdSigner
	func() {
		// wrap so apps list is lazily evaluated
		defer func() {
			GracefulShutdown(suite.apps)
		}()
		defer suite.ClosePoet()
		defer clock.RealClose()

		for _, a := range suite.apps {
			suite.NoError(a.startServices(context.TODO(), logtest.New(suite.T())))
		}

		ActivateGrpcServer(suite.apps[0])

		if err := poetHarness.Start(context.TODO(), []string{fmt.Sprintf("127.0.0.1:%d", suite.apps[0].grpcAPIService.Port)}); err != nil {
			suite.T().Fatalf("failed to start poet server: %v", err)
		}

		timeout := time.After(10 * time.Minute)

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

	// initialize a new app using the same database as the first node and make sure the state roots match
	smApp, err := InitSingleInstance(lg, *cfg, 0, genesisTime, firstDir, rolacle, poetHarness.HTTPPoetClient, clock, net, edSgn)
	suite.NoError(err)
	// test that loaded root is equal
	suite.validateReloadStateRoot(smApp, oldRoot)
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
			suite.Error(err)
			log.With().Info("got expected error submitting tx with out of order nonce", log.Err(err))
		}
	}

	test := func(suite *AppTestSuite, t *testing.T) bool {
		ok := true
		for _, app := range suite.apps {
			log.With().Info("zero acc current balance",
				app.nodeID,
				log.FieldNamed("sender", addr),
				log.FieldNamed("dest", dst),
				log.Uint64("dest_balance", app.state.GetBalance(dst)),
				log.Uint64("sender_nonce", app.state.GetNonce(addr)),
				log.Uint64("sender_balance", app.state.GetBalance(addr)))
			ok = ok && 0 == app.state.GetBalance(dst) && app.state.GetNonce(addr) == 0
		}
		return ok
	}

	return TestScenario{setup, test, dependencies}
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

	test := func(suite *AppTestSuite, t *testing.T) bool {
		ok := true
		for _, app := range suite.apps {
			log.With().Info("valid tx recipient acc current balance",
				app.nodeID,
				log.FieldNamed("sender", addr),
				log.FieldNamed("dest", dst),
				log.Uint64("dest_balance", app.state.GetBalance(dst)),
				log.Uint64("sender_nonce", app.state.GetNonce(addr)),
				log.Uint64("sender_balance", app.state.GetBalance(addr)))
			ok = ok && app.state.GetBalance(dst) >= 250 && app.state.GetNonce(addr) == uint64(txsSent)
		}
		return ok
	}

	return TestScenario{setup, test, dependencies}
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

func healingTester(dependencies []int) TestScenario {
	var lastVerifiedLayer types.LayerID
	once := sync.Once{}
	setup := func(suite *AppTestSuite, t *testing.T) {}

	test := func(suite *AppTestSuite, t *testing.T) bool {
		// we can't actually use setup() as it's destructive to other tests, so run setup here
		once.Do(func() {
			// immediately kill half of the participating nodes
			// poet communicates with GRPC server on the first app, so leave first one alone
			// we need to kill at least half, so round up
			firstAppToKill := len(suite.apps) - (len(suite.apps)/2 + 1)

			// save data on the nodes we're about to kill for posterity
			suite.killedApps = suite.apps[firstAppToKill:]
			GracefulShutdown(suite.apps[firstAppToKill:])

			// make sure we don't attempt to shut down the same apps twice
			suite.apps = suite.apps[:firstAppToKill]

			lastVerifiedLayer = suite.apps[0].mesh.LatestLayerInState()
			suite.killEpoch = lastVerifiedLayer.GetEpoch()
		})

		// now wait for healing to kick in and advance the verified layer
		for i, app := range suite.apps {
			lyrReceived := app.mesh.ProcessedLayer()
			lyrVerified := app.mesh.LatestLayerInState()
			log.With().Info("node latest layers",
				log.FieldNamed("last_received", lyrReceived),
				log.FieldNamed("last_verified", lyrVerified),
				log.Int("app", i),
				log.FieldNamed("nodeid", app.nodeID))

			// verified needs to advance, and to nearly catch up to last received
			if !lyrVerified.After(lastVerifiedLayer.Add(2)) || lyrVerified.Add(2).Before(lyrReceived) {
				return false
			}
		}
		log.Info("healing okay")
		return true
	}
	return TestScenario{setup, test, dependencies}
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

func (suite *AppTestSuite) validateReloadStateRoot(app *SpacemeshApp, oldRoot types.Hash32) {
	// test that loaded root is equal
	assert.Equal(suite.T(), oldRoot, app.state.GetStateRoot())

	// start and stop and test for no panics
	suite.NoError(app.startServices(context.TODO()))
	app.stopServices()
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

	for i, d := range datamap {
		log.Info("node %v in len(layerstoblocks) %v", i, len(d.layertoblocks))
		if i == lateNodeKey { // skip late node
			continue
		}
		for i2, d2 := range datamap {
			if i == i2 {
				continue
			}

			assert.Equal(suite.T(), len(d.layertoblocks), len(d2.layertoblocks), "%v layer block count mismatch with %v: %v not %v", i, i2, len(d.layertoblocks), len(d2.layertoblocks))

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
	nodedata := datamap[suite.apps[0].nodeID.Key]

	lastLayer := len(nodedata.layertoblocks) + 5
	log.Info("node %v", suite.apps[0].nodeID.ShortString())

	totalBlocks := 0
	for id, l := range nodedata.layertoblocks {
		totalBlocks += len(l)
		log.Info("node %v layer %v, blocks %v", suite.apps[0].nodeID.ShortString(), id, len(l))
	}

	genesisBlocks := 0
	for i := uint32(0); i < layersPerEpoch*2; i++ {
		if l, ok := nodedata.layertoblocks[types.NewLayerID(i)]; ok {
			genesisBlocks += len(l)
		}
	}

	// assert number of blocks
	totalEpochs := int(untilLayer.GetEpoch()) + 1
	blocksPerEpochTarget := layerAvgSize * int(layersPerEpoch)

	allApps := append(suite.apps, suite.killedApps...)
	expectedEpochWeight := configuredTotalWeight(allApps)

	// note: expected no. blocks per epoch should remain the same even after some nodes are killed, since the
	// remaining nodes will be eligible for proportionally more blocks per layer
	expectedBlocksPerEpoch := 0
	for _, app := range allApps {
		expectedBlocksPerEpoch += max(blocksPerEpochTarget*int(app.Config.SMESHING.Opts.NumUnits)/int(expectedEpochWeight), 1)
	}

	// note: we expect the number of blocks to be a bit less than the expected number since, after some apps were
	// killed and before healing kicked in, some blocks should be missing

	expectedTotalBlocks := (totalEpochs - 2) * expectedBlocksPerEpoch
	actualTotalBlocks := totalBlocks - genesisBlocks
	suite.Equal(expectedTotalBlocks, actualTotalBlocks,
		fmt.Sprintf("got unexpected block count! got: %v, want: %v. totalBlocks: %v, genesisBlocks: %v, lastLayer: %v, layersPerEpoch: %v layerAvgSize: %v totalEpochs: %v datamap: %v",
			actualTotalBlocks, expectedTotalBlocks, totalBlocks, genesisBlocks, lastLayer, layersPerEpoch, layerAvgSize, totalEpochs, datamap))

	totalWeightAllEpochs := calcTotalWeight(assert.New(suite.T()), suite.apps[0].atxDb, allApps, types.EpochID(totalEpochs))

	// assert total ATX weight
	expectedTotalWeight := uint64(totalEpochs) * expectedEpochWeight
	suite.Equal(int(expectedTotalWeight), int(totalWeightAllEpochs),
		fmt.Sprintf("total atx weight is wrong, got: %v, want: %v\n"+
			"totalEpochs: %v, numApps: %v, expectedWeight: %v",
			totalWeightAllEpochs, expectedTotalWeight, totalEpochs, len(allApps), expectedEpochWeight))
}

func max(i, j int) int {
	if i > j {
		return i
	}
	return j
}

func calcTotalWeight(
	assert *assert.Assertions,
	atxDb *activation.DB,
	apps []*App,
	untilEpoch types.EpochID) (totalWeightAllEpochs uint64) {
	for _, app := range apps {
		atxID, err := atxDb.GetNodeLastAtxID(app.nodeID)
		assert.NoError(err)

		for atxID != *types.EmptyATXID {
			atx, err := atxDb.GetAtxHeader(atxID)
			assert.NoError(err)
			if atx.TargetEpoch() < untilEpoch+2 {
				totalWeightAllEpochs += atx.GetWeight()
				log.With().Info("added atx weight",
					log.FieldNamed("pub_layer", atx.PubLayerID),
					log.FieldNamed("target_epoch", atx.TargetEpoch()),
					log.Uint64("weight", atx.GetWeight()))
			} else {
				log.With().Info("ignoring atx after final epoch",
					log.FieldNamed("pub_layer", atx.PubLayerID),
					log.FieldNamed("target_epoch", atx.TargetEpoch()),
					log.Uint64("weight", atx.GetWeight()))
			}
			atxID = atx.PrevATXID
		}
	}
	return
}

func (suite *AppTestSuite) validateLastATXTotalWeight(app *App, numberOfEpochs int, expectedTotalWeight uint64) {
	atxs := app.atxDb.GetEpochAtxs(types.EpochID(numberOfEpochs - 1))
	suite.Len(atxs, len(suite.apps), "node: %v", app.nodeID.ShortString())

	totalWeight := uint64(0)
	for _, atxID := range atxs {
		atx, _ := app.atxDb.GetAtxHeader(atxID)
		totalWeight += atx.GetWeight()
	}
	suite.Equal(int(expectedTotalWeight), int(totalWeight), "node: %v", app.nodeID.ShortString())
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
	smApp.Config.SMESHING.Opts.DataDir, _ = ioutil.TempDir("", "sm-test-post")
	smApp.Config.SMESHING.Opts.ComputeProviderID = initialization.CPUProviderID()

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
	smApp.Config.FETCH.MaxRetriesForPeer = 5

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
	r.NoError(smApp.initServices(context.TODO(), nodeID, swarm, dbStorepath, edSgn, false, hareOracle, uint32(smApp.Config.LayerAvgSize), poetHarness.HTTPPoetClient, vrfSigner, smApp.Config.LayersPerEpoch, clock))
	r.NoError(smApp.startServices(context.TODO(), logtest.New(t)))
	ActivateGrpcServer(smApp)

	r.NoError(poetHarness.Teardown(true))
	smApp.stopServices()

	time.Sleep(5 * time.Second)
	gCount2 := runtime.NumGoroutine()

	if gCount != gCount2 {
		buf := make([]byte, 1<<16)
		numbytes := runtime.Stack(buf, true)
		logtest.New(t).Error(string(buf[:numbytes]))
	}
	require.Equal(t, gCount, gCount2)
}
