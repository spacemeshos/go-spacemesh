package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/rs/cors"
	metricsProm "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	"github.com/slok/go-http-metrics/middleware/std"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

// JSONHTTPServer is a JSON http server providing the Spacemesh API.
// It is implemented using a grpc-gateway. See https://github.com/grpc-ecosystem/grpc-gateway .
type JSONHTTPServer struct {
	listener       string
	collectMetrics bool
	logger         *zap.Logger

	// BoundAddress contains the address that the server bound to, useful if
	// the server uses a dynamic port. It is set during startup and can be
	// safely accessed after Start has completed (I.E. the returned channel has
	// been waited on)
	BoundAddress string
	server       *http.Server
	eg           errgroup.Group

	// basic CORS support
	origins []string
}

// NewJSONHTTPServer creates a new json http server.
func NewJSONHTTPServer(
	lg *zap.Logger,
	listener string,
	corsAllowedOrigins []string,
	collectMetrics bool,
) *JSONHTTPServer {
	return &JSONHTTPServer{
		logger:         lg,
		listener:       listener,
		origins:        corsAllowedOrigins,
		collectMetrics: collectMetrics,
	}
}

// Shutdown stops the server.
func (s *JSONHTTPServer) Shutdown(ctx context.Context) error {
	s.logger.Debug("stopping json-http service...")
	if s.server != nil {
		err := s.server.Shutdown(ctx)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	err := s.eg.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// StartService starts the json api server and listens for status (started, stopped).
func (s *JSONHTTPServer) StartService(
	services ...ServiceAPI,
) error {
	// At least one service must be enabled
	if len(services) == 0 {
		s.logger.Error("not starting grpc gateway service; at least one service must be enabled")
		return errors.New("no services provided")
	}

	// setup metrics middleware
	var mdlws []runtime.Middleware
	if s.collectMetrics {
		mdlw := middleware.New(middleware.Config{
			Recorder: metricsProm.NewRecorder(metricsProm.Config{
				Prefix: metrics.Namespace + "_api",
			}),
		})
		mdlws = append(mdlws, func(next runtime.HandlerFunc) runtime.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
				wrappedNext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					next(w, r, pathParams)
				})
				std.Handler("", mdlw, wrappedNext).ServeHTTP(w, r)
			}
		})
	}

	// register each individual, enabled service
	mux := runtime.NewServeMux(runtime.WithMiddlewares(mdlws...))

	for _, svc := range services {
		if err := svc.RegisterHandlerService(mux); err != nil {
			return fmt.Errorf("registering service %s with grpc gateway failed: %w", svc, err)
		}
	}

	// enable cors
	c := cors.New(cors.Options{
		AllowedOrigins: s.origins,
	})

	s.logger.Info("starting grpc gateway server", zap.String("address", s.listener))
	lis, err := net.Listen("tcp", s.listener)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.listener, err)
	}
	s.BoundAddress = lis.Addr().String()
	s.server = &http.Server{
		MaxHeaderBytes: 1 << 21,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		Handler:        c.Handler(mux),
	}
	s.eg.Go(func() error {
		if err := s.server.Serve(lis); err != nil {
			s.logger.Error("serving grpc server", zap.Error(err))
			return nil
		}
		return nil
	})
	return nil
}
