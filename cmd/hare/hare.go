package main

import (
	"fmt"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spf13/cobra"
	"os"
	"time"
)

const defaultSetSize = 200

// VersionCmd returns the current version of spacemesh
var Cmd = &cobra.Command{
	Use:   "hare",
	Short: "start hare",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting hare")
		hareApp := NewHareApp()
		defer hareApp.Cleanup()

		hareApp.Initialize(cmd)
		hareApp.Start(cmd, args)
		<-hareApp.proc.CloseChannel()
	},
}

func init() {
	cmdp.AddCommands(Cmd)
}

type HareApp struct {
	*cmdp.BaseApp
	p2p    p2p.Service
	broker *hare.Broker
	proc   *hare.ConsensusProcess
	oracle *oracle.OracleClient
	sgn    hare.Signing
}

func NewHareApp() *HareApp {
	return &HareApp{BaseApp: cmdp.NewBaseApp(), sgn: hare.NewMockSigning()}
}

func (app *HareApp) Cleanup() {
	// TODO: move to array of cleanup functions and execute all here
	app.oracle.Unregister(true, app.sgn.Verifier().String())
}

func buildSet() *hare.Set {
	s := hare.NewEmptySet(defaultSetSize)

	for i := uint64(0); i < defaultSetSize; i++ {
		s.Add(hare.NewValue(i))
	}

	return s
}

func (app *HareApp) Start(cmd *cobra.Command, args []string) {
	// start p2p services
	log.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)
	app.p2p = swarm
	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		panic("Error starting p2p services")
	}

	pub := app.sgn.Verifier()

	lg := log.NewDefault(pub.String())

	oracle.SetServerAddress(app.Config.OracleServer)
	app.oracle = oracle.NewOracleClientWithWorldID(uint64(app.Config.OracleServerWorldId))
	app.oracle.Register(true, pub.String()) // todo: configure no faulty nodes
	hareOracle := oracle.NewHareOracleFromClient(app.oracle)

	broker := hare.NewBroker(swarm, hare.NewEligibilityValidator(hare.NewHareOracle(hareOracle, app.Config.HARE.N), lg))
	app.broker = broker
	broker.Start()
	app.p2p.Start()

	time.Sleep(10 * time.Second)

	proc := hare.NewConsensusProcess(app.Config.HARE, 1, buildSet(), hareOracle, app.sgn, swarm, make(chan hare.TerminationOutput, 1), lg)
	app.proc = proc
	proc.SetInbox(broker.Register(proc.Id()))
	proc.Start()
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
