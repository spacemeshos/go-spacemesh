package main

import (
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/require"
	"runtime"
	"testing"
	"time"
)

func activateGrpcServer(smApp *SpacemeshApp) {
	smApp.Config.API.StartGrpcServer = true
	smApp.grpcAPIService = api.NewGrpcService(smApp.P2P, smApp.state, smApp.txProcessor, smApp.atxBuilder, smApp.oracle)
	smApp.grpcAPIService.StartService()
}

var net = service.NewSimulator()

func TestShutdown(t *testing.T) {

	g_count := runtime.NumGoroutine()

	//defer leaktest.Check(t)()
	r := require.New(t)

	smApp := NewSpacemeshApp()
	genesisTime := time.Now().Add(time.Second * 10)
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
	smApp.Config.CoinbaseAccount = "0x123"
	smApp.Config.LayerAvgSize = 5
	smApp.Config.LayersPerEpoch = 3
	smApp.Config.Hdist = 5
	smApp.Config.GenesisTime = genesisTime.Format(time.RFC3339)
	smApp.Config.LayerDurationSec = 20
	smApp.Config.HareEligibility.ConfidenceParam = 3
	smApp.Config.HareEligibility.EpochOffset = 0
	smApp.Config.StartMining = true

	rolacle := eligibility.New()

	edSgn := signing.NewEdSigner()
	pub := edSgn.PublicKey()

	poetClient, err := NewRPCPoetHarnessClient()
	if err != nil {
		log.Panic("failed creating poet client harness: %v", err)
	}

	vrfPriv, vrfPub := BLS381.GenKeyPair(BLS381.DefaultSeed())
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)
	nodeID := types.NodeId{Key: pub.String(), VRFPublicKey: vrfPub}

	swarm := net.NewNode()
	dbStorepath := "/tmp/" + pub.String()

	hareOracle := oracle.NewLocalOracle(rolacle, 5, nodeID)
	hareOracle.Register(true, pub.String())

	postClient := nipst.NewPostClient(&smApp.Config.POST)

	err = smApp.initServices(nodeID, swarm, dbStorepath, edSgn, false, hareOracle, uint32(smApp.Config.LayerAvgSize), postClient, poetClient, vrfSigner, uint16(smApp.Config.LayersPerEpoch))

	r.NoError(err)
	smApp.setupGenesis()

	smApp.startServices()
	activateGrpcServer(smApp)

	poetClient.CleanUp()
	smApp.stopServices()

	time.Sleep(3 * time.Second)
	g_count2 := runtime.NumGoroutine()

	require.Equal(t, g_count, g_count2)
}
