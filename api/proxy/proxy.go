package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
)

type Server struct {
	BoundAddress string

	logger     *zap.Logger
	listener   string
	httpServer *http.Server
	errGroup   errgroup.Group
}

type Service interface {
	grpcserver.ServiceAPI
	Path() string
}

func NewServer(proxyListener, apiAddress string, logger *zap.Logger, local ...Service) (*Server, error) {
	// Validate the API server URL
	targetURL, err := url.Parse(apiAddress)
	if err != nil {
		return nil, err
	}

	// Create a reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	mux := http.NewServeMux()

	// Register GRPC services handled locally
	grpcMux := runtime.NewServeMux()
	for _, svc := range local {
		svc.RegisterHandlerService(grpcMux)
		mux.Handle(svc.Path(), grpcMux)
	}

	// The rest is proxied.
	// HTTP handler to forward requests
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		r.Host = targetURL.Host
		proxy.ServeHTTP(w, r)
	})

	// Initialize the HTTP server
	server := &http.Server{
		Addr:         proxyListener,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return &Server{
		logger:     logger,
		listener:   proxyListener,
		httpServer: server,
	}, nil
}

func (s *Server) Start() error {
	s.logger.Info("starting proxy server", zap.String("address", s.listener))

	lis, err := net.Listen("tcp", s.listener)
	if err != nil {
		s.logger.Error("start proxy listen server", zap.Error(err))
		return err
	}
	s.BoundAddress = lis.Addr().String()

	s.errGroup.Go(func() error {
		return s.httpServer.Serve(lis)
	})

	return nil
}

func (s *Server) Stop() error {
	s.logger.Info("stopping proxy server")
	s.errGroup.Go(func() error {
		return s.httpServer.Shutdown(context.Background())
	})
	return s.errGroup.Wait()
}
