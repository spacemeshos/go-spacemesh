// package p2p cmd is the main executable for running p2p tests and simulations
package main

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/api"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	_ "net/http/pprof"
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

// P2PApp is an app struct used to run only p2p functionality
type P2PApp struct {
	*cmdp.BaseApp
	p2p     p2p.Service
	closers []io.Closer
}

// NewP2PApp creates the base app
func NewP2PApp() *P2PApp {
	return &P2PApp{BaseApp: cmdp.NewBaseApp()}
}

// Cleanup closes all services
func (app *P2PApp) Cleanup() {
	for _, c := range app.closers {
		err := c.Close()
		if err != nil {
			log.Warning("Error when closing service err=%v")
		}
	}
	// TODO: move to array of cleanup functions and execute all here
}

// Start creates a p2p instance and starts it with testing features enabled by the config.
func (app *P2PApp) Start(cmd *cobra.Command, args []string) {
	// init p2p services
	log.JSONLog(true)
	log.DebugMode(true)

	log.Info("Initializing P2P services")

	logger := log.NewDefault("P2P_Test")

	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P, logger, app.Config.DataDir)
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

	if app.Config.PprofHttpServer {
		pprof := &http.Server{}
		pprof.Addr = ":6060"
		pprof.Handler = nil
		go func() { err := pprof.ListenAndServe(); log.Error("error running pprof server err=%v", err) }()
		app.closers = append(app.closers, pprof)
	}

	// start api servers
	if app.Config.API.StartGrpcServer || app.Config.API.StartJSONServer {
		// start grpc if specified or if json rpc specified
		log.Info("Started the GRPC Service")
		grpc := api.NewGrpcService(app.Config.API.GrpcServerPort, app.p2p, nil, nil, nil, nil, nil, nil, nil, 0, nil, nil, nil)
		grpc.StartService()
		app.closers = append(app.closers, grpc)
	}

	if app.Config.API.StartJSONServer {
		log.Info("Started the JSON Service")
		json := api.NewJSONHTTPServer(app.Config.API.JSONServerPort, app.Config.API.GrpcServerPort)
		json.StartService()
		app.closers = append(app.closers, json)
	}

	<-cmdp.Ctx.Done()
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
