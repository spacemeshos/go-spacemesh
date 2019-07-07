package main

import (
	"fmt"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spf13/cobra"
	"os"
	"time"
)

const defaultSetSize = 200

// Hare cmd
var Cmd = &cobra.Command{
	Use:   "hare",
	Short: "start hare",
	Run: func(cmd *cobra.Command, args []string) {
		log.JSONLog(true)
		hareApp := NewHareApp()
		defer hareApp.Cleanup()
		hareApp.Initialize(cmd)
		hareApp.Start(cmd, args)
		<-hareApp.ha.CloseChannel()
	},
}

func init() {
	cmdp.AddCommands(Cmd)
}

type mockBlockProvider struct {
	isPulled bool
}

func (mbp *mockBlockProvider) GetUnverifiedLayerBlocks(layerId types.LayerID) ([]types.BlockID, error) {
	if mbp.isPulled {
		return []types.BlockID{}, nil
	}

	mbp.isPulled = true
	return []types.BlockID{1, 2, 3}, nil
}

type HareApp struct {
	*cmdp.BaseApp
	p2p    p2p.Service
	oracle *oracle.OracleClient
	sgn    hare.Signer
	ha     *hare.Hare
	clock  *timesync.Ticker
}

func IsSynced() bool {
	return true
}

func NewHareApp() *HareApp {
	return &HareApp{BaseApp: cmdp.NewBaseApp(), sgn: signing.NewEdSigner()}
}

func (app *HareApp) Cleanup() {
	// TODO: move to array of cleanup functions and execute all here
	app.oracle.Unregister(true, app.sgn.PublicKey().String())
}

func buildSet() *hare.Set {
	s := hare.NewEmptySet(defaultSetSize)

	for i := uint64(0); i < defaultSetSize; i++ {
		s.Add(hare.NewValue(i))
	}

	return s
}

type mockIdProvider struct {
}

func (mip *mockIdProvider) GetIdentity(edId string) (types.NodeId, error) {
	return types.NodeId{Key: edId, VRFPublicKey: []byte{}}, nil
}

type mockStateQuerier struct {
}

func (msq mockStateQuerier) IsIdentityActive(edId string, layer types.LayerID) (bool, types.AtxId, error) {
	return true, *types.EmptyAtxId, nil
}

func (app *HareApp) Start(cmd *cobra.Command, args []string) {
	log.Info("Starting hare main")
	log.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)
	app.p2p = swarm
	if err != nil {
		log.Panic("Error starting p2p services err=%v", err)
	}

	pub := app.sgn.PublicKey()

	lg := log.NewDefault(pub.String())

	oracle.SetServerAddress(app.Config.OracleServer)
	app.oracle = oracle.NewOracleClientWithWorldID(uint64(app.Config.OracleServerWorldId))
	app.oracle.Register(true, pub.String()) // todo: configure no faulty nodes
	hareOracle := oracle.NewHareOracleFromClient(app.oracle)

	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	if err != nil {
		log.Panic("error parsing config err=%v", err)
	}
	ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	app.clock = timesync.NewTicker(timesync.RealClock{}, ld, gTime)

	app.ha = hare.New(app.Config.HARE, app.p2p, app.sgn, types.NodeId{Key: app.sgn.PublicKey().String(), VRFPublicKey: []byte{}}, IsSynced, &mockBlockProvider{}, hareOracle, uint16(app.Config.CONSENSUS.LayersPerEpoch), &mockIdProvider{}, &mockStateQuerier{}, app.clock.Subscribe(), lg)
	log.Info("Starting hare service")
	err = app.ha.Start()
	if err != nil {
		log.Panic("error starting maatuf err=%v", err)
	}
	err = app.p2p.Start()
	if err != nil {
		log.Panic("error starting p2p err=%v", err)
	}
	app.clock.Start()
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
