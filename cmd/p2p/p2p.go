// package p2p cmd is the main executable for running p2p tests and simulations
package main

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
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
		if err := c.Close(); err != nil {
			log.With().Warning("error closing service", log.Err(err))
		}
	}
	// TODO: move to array of cleanup functions and execute all here
}

// Start creates a p2p instance and starts it with testing features enabled by the config.
func (app *P2PApp) Start(cmd *cobra.Command, args []string) {
	// init p2p services
	log.JSONLog(true)
	log.DebugMode(true)

	log.Info("initializing p2p services")

	logger := log.NewDefault("P2P_Test")

	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P, logger, app.Config.DataDir())
	if err != nil {
		log.With().Panic("error init p2p services", log.Err(err))
	}
	app.p2p = swarm

	// Testing stuff
	api.ApproveAPIGossipMessages(cmdp.Ctx, app.p2p)
	metrics.StartMetricsServer(app.Config.MetricsPort)

	// start the node

	err = app.p2p.Start(cmdp.Ctx)
	defer app.p2p.Shutdown()

	if err != nil {
		log.With().Panic("error starting p2p services", log.Err(err))
	}

	if app.Config.PprofHTTPServer {
		pprof := &http.Server{}
		pprof.Addr = ":6060"
		pprof.Handler = nil
		go func() { err := pprof.ListenAndServe(); log.With().Error("error running pprof server", log.Err(err)) }()
		app.closers = append(app.closers, pprof)
	}

	app.startAPI()

	<-cmdp.Ctx.Done()
}

func (app *P2PApp) startAPI() {
	apiConf := &app.Config.API

	// Make sure we only create the server once.
	var grpcSvc *grpcserver.Server
	registerService := func(svc grpcserver.ServiceAPI) {
		if grpcSvc == nil {
			grpcSvc = grpcserver.NewServerWithInterface(apiConf.GrpcServerPort, apiConf.GrpcServerInterface)
		}
		svc.RegisterService(grpcSvc)
	}

	// Register the requested services one by one
	// We only support a subset of API services: gateway, globalstate, transaction
	if apiConf.StartMeshService || apiConf.StartNodeService || apiConf.StartSmesherService {
		log.Panic("unsupported grpc api service requested")
	}
	if apiConf.StartGatewayService {
		registerService(grpcserver.NewGatewayService(app.p2p))
	}
	if apiConf.StartGlobalStateService {
		registerService(grpcserver.NewGlobalStateService(nil, nil))
	}
	if apiConf.StartTransactionService {
		registerService(grpcserver.NewTransactionService(app.p2p, nil, nil, nil))
	}

	// Now that the services are registered, start the server.
	if grpcSvc != nil {
		grpcSvc.Start()
		app.closers = append(app.closers, grpcSvc)
	}

	var jsonSvc *grpcserver.JSONHTTPServer
	if apiConf.StartJSONServer {
		if grpcSvc == nil {
			// This panics because it should not happen.
			// It should be caught inside apiConf.
			log.Panic("one or more new grpc services must be enabled with new json gateway server")
		}
		jsonSvc = grpcserver.NewJSONHTTPServer(apiConf.JSONServerPort, apiConf.GrpcServerPort)
		jsonSvc.StartService(
			cmdp.Ctx,
			apiConf.StartDebugService,
			apiConf.StartGatewayService,
			apiConf.StartGlobalStateService,
			apiConf.StartMeshService,
			apiConf.StartNodeService,
			apiConf.StartSmesherService,
			apiConf.StartTransactionService,
		)
		app.closers = append(app.closers, jsonSvc)
	}
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
