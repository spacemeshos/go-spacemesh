package metrics

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"

	spacemesh "github.com/spacemeshos/go-spacemesh/metrics"
)

const subsystem = "beacons"

type Server struct {
	started chan struct{}
	stopped chan struct{}
	server  *http.Server
	eg      errgroup.Group

	metric *prometheus.GaugeVec
}

func NewServer(addr string) (*Server, error) {
	// Create a new Gauge metric to be pushed to the Prometheus server
	metric := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName(spacemesh.Namespace, subsystem, "beacon_alarm"),
		Help: "Alarm if consensus on beacon value is not reached.",
	}, []string{"epoch"})

	// Register the metric with the default registry
	if err := prometheus.Register(metric); err != nil {
		return nil, fmt.Errorf("failed to register metric: %w", err)
	}

	return &Server{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
		server: &http.Server{
			Addr:    addr,
			Handler: promhttp.Handler(),
		},
		metric: metric,
	}, nil
}

func (s *Server) Start() error {
	select {
	case <-s.started:
		return fmt.Errorf("server already started")
	default:
		close(s.started)
	}

	s.eg.Go(func() error {
		log.Println("Starting Prometheus metrics server at :8080/metrics")
		err := s.server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			close(s.stopped)
			log.Println("Failed to start Prometheus metrics server:", err)
			return err
		}
		return nil
	})
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	select {
	case <-s.stopped:
		return fmt.Errorf("server already stopped")
	default:
		close(s.stopped)
	}

	// Shutdown the server gracefully
	if err := s.server.Shutdown(ctx); err != nil {
		log.Println("Error while shutting down Prometheus metrics server:", err)
		return err
	}
	if err := s.eg.Wait(); err != nil {
		log.Println("Error while waiting for shut down of Prometheus metrics server:", err)
		return err
	}
	log.Println("Prometheus metrics server stopped.")
	return nil
}

func (s *Server) UpdateAlarm(epoch string, alarm bool) error {
	select {
	case <-s.started:
	default:
		return fmt.Errorf("server not started")
	}

	select {
	case <-s.stopped:
		return fmt.Errorf("server already stopped")
	default:
	}

	gauge := s.metric.With(prometheus.Labels{"epoch": epoch})
	if alarm {
		gauge.Set(1)
		return nil
	}
	gauge.Set(0)
	return nil
}
