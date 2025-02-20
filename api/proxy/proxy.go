package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/rs/cors"
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

func NewServer(
	proxyListener, apiAddress string,
	corsEverywhere bool,
	logger *zap.Logger,
	local ...Service,
) (*Server, error) {
	// Validate the API server URL
	targetURL, err := url.Parse(apiAddress)
	if err != nil {
		return nil, err
	}

	// Create a reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	mux := http.NewServeMux()
	var handler http.Handler = mux
	if corsEverywhere {
		logger.Info("enabling CORS on PROXY for all origins")
		c := cors.New(cors.Options{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD"},
			AllowedHeaders: []string{"*"},
			ExposedHeaders: []string{
				"Server",
				"Date",
				"Content-Type",
				"Content-Length",
				"Connection",
				"Vary",
				"X-Final-Url",
				"Access-Control-Allow-Origin",
			},
			AllowCredentials: false,
			MaxAge:           300,
		})
		handler = c.Handler(mux)

		proxy.ModifyResponse = func(resp *http.Response) error {
			// Remove CORS headers from the target response
			for k := range resp.Header {
				if strings.HasPrefix(k, "Access-Control-") {
					delete(resp.Header, k)
				}
			}
			return nil
		}
	}

	// Register GRPC services handled locally
	grpcMux := runtime.NewServeMux()
	for _, svc := range local {
		if err := svc.RegisterHandlerService(grpcMux); err != nil {
			return nil, fmt.Errorf("registering local service %s: %w", svc.Path(), err)
		}
		mux.Handle(svc.Path(), grpcMux)
	}

	// The rest is proxied.
	// HTTP handler to forward requests
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	})

	// Initialize the HTTP server
	server := &http.Server{
		Addr:         proxyListener,
		Handler:      handler,
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
