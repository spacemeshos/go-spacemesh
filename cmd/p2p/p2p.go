// package p2p cmd is the main executable for running p2p tests and simulations
package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

// Cmd is the p2p cmd.
var Cmd = &cobra.Command{
	Use:   "p2p",
	Short: "start p2p",
	Run: func(cmd *cobra.Command, args []string) {
		p2pApp := NewP2PApp()

		p2pApp.Initialize(cmd)
		p2pApp.Start(cmd, args)
	},
}

func init() {
	cmdp.AddCommands(Cmd)
}

// P2PApp is an app struct used to run only p2p functionality.
type P2PApp struct {
	*cmdp.BaseApp
}

// NewP2PApp creates the base app.
func NewP2PApp() *P2PApp {
	return &P2PApp{BaseApp: cmdp.NewBaseApp()}
}

// Start creates a p2p instance and starts it with testing features enabled by the config.
func (app *P2PApp) Start(cmd *cobra.Command, args []string) {
	// init p2p services
	log.JSONLog(true)
	logger := log.NewWithLevel("P2P_Test", zap.NewAtomicLevelAt(zap.InfoLevel))
	log.SetupGlobal(logger)

	log.Info("initializing p2p services")

	cfg := app.Config.P2P
	cfg.DataDir = filepath.Join(app.Config.DataDir(), "p2p")
	ctx := cmdp.Ctx()
	host, err := p2p.New(ctx, logger, cfg)
	if err != nil {
		log.With().Panic("failed to create host", log.Err(err))
	}
	defer host.Stop()

	host.Register(activation.PoetProofProtocol, func(ctx context.Context, pid p2p.Peer, msg []byte) pubsub.ValidationResult {
		log.With().Info("api_test_gossip: got test gossip message", log.Int("len", len(msg)))
		return pubsub.ValidationAccept
	})
	metrics.StartMetricsServer(app.Config.MetricsPort)
	if app.Config.PprofHTTPServer {
		pprof := &http.Server{}
		pprof.Addr = ":6060"
		pprof.Handler = nil
		go func() { err := pprof.ListenAndServe(); log.With().Error("error running pprof server", log.Err(err)) }()
		defer pprof.Close()
	}
	svc := grpcserver.NewServerWithInterface(app.Config.API.GrpcServerPort, app.Config.API.GrpcServerInterface)
	gwsvc := grpcserver.NewGatewayService(host)
	gwsvc.RegisterService(svc)
	svc.Start()
	defer svc.Close()

	jsonSvc := grpcserver.NewJSONHTTPServer(app.Config.API.JSONServerPort)
	jsonSvc.StartService(ctx, gwsvc)
	defer jsonSvc.Close()

	<-ctx.Done()
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
