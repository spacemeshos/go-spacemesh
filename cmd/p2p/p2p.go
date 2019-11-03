package main

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/api"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spf13/cobra"
	"os"
)

// Cmd is the p2p cmd
var Cmd = &cobra.Command{
	Use:   "p2p",
	Short: "start p2p",
	Run: func(cmd *cobra.Command, args []string) {
		p2pApp := NewP2PApp()
		defer p2pApp.Cleanup()

		p2pApp.Initialize(cmd)
		p2pApp.Start(cmd, args)
	},
}

func init() {
	cmdp.AddCommands(Cmd)
}

type P2PApp struct {
	*cmdp.BaseApp
	p2p p2p.Service
}

func NewP2PApp() *P2PApp {
	return &P2PApp{BaseApp: cmdp.NewBaseApp()}
}

func (app *P2PApp) Cleanup() {
	// TODO: move to array of cleanup functions and execute all here
}

func (app *P2PApp) Start(cmd *cobra.Command, args []string) {
	// init p2p services
	log.JSONLog(true)
	log.DebugMode(true)
	log.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)
	if err != nil {
		log.Panic("Error init p2p services, err: %v", err)
	}
	app.p2p = swarm

	// Testing stuff
	api.ApproveAPIGossipMessages(cmdp.Ctx, app.p2p)
	metrics.StartCollectingMetrics(app.Config.MetricsPort)

	// start the node

	err = app.p2p.Start()
	defer app.p2p.Shutdown()

	if err != nil {
		log.Panic("Error starting p2p services, err: %v", err)
	}

	// start api servers
	if app.Config.API.StartGrpcServer || app.Config.API.StartJSONServer {
		// start grpc if specified or if json rpc specified
		log.Info("Started the GRPC Service")
		grpc := api.NewGrpcService(app.p2p, nil, nil, nil, nil, nil, nil)
		grpc.StartService()
	}

	if app.Config.API.StartJSONServer {
		log.Info("Started the JSON Service")
		json := api.NewJSONHTTPServer()
		json.StartService()
	}

	<-cmdp.Ctx.Done()
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
