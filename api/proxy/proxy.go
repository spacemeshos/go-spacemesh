package proxy

import (
	"context"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

type Server struct {
	logger     *zap.Logger
	listener   string
	apiAddress string
	httpServer *http.Server
	errGroup   errgroup.Group
}

func NewServer(proxyListener string, apiAddress string, logger *zap.Logger) (*Server, error) {
	// Validate the API server URL
	targetURL, err := url.Parse(apiAddress)
	if err != nil {
		return nil, err
	}

	// Create a reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// HTTP handler to forward requests
	mux := http.NewServeMux()
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
		apiAddress: apiAddress,
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

	s.errGroup.Go(func() error {
		if err := s.httpServer.Serve(lis); err != nil {
			return err
		}
		return nil
	})

	return nil
}

func (s *Server) Stop() error {
	s.logger.Info("stopping proxy server")
	s.errGroup.Go(func() error {
		if err := s.httpServer.Shutdown(context.Background()); err != nil {
			return err
		}
		return nil
	})
	return s.errGroup.Wait()
}
