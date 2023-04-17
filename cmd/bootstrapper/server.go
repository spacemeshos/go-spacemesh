package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

var bootstrapEpoch = types.EpochID(2)

type NetworkParam struct {
	Genesis      time.Time
	LyrsPerEpoch uint32
	LyrDuration  time.Duration
	Offset       uint32
}

func (np *NetworkParam) updateBeaconTime(targetEpoch types.EpochID) time.Time {
	start := np.Genesis.Add(time.Duration(targetEpoch) * time.Duration(np.LyrsPerEpoch) * np.LyrDuration)
	return start.Add(-1 * time.Duration(np.Offset) * np.LyrDuration)
}

func (np *NetworkParam) updateActiveSetTime(targetEpoch types.EpochID) time.Time {
	start := np.Genesis.Add(time.Duration(targetEpoch) * time.Duration(np.LyrsPerEpoch) * np.LyrDuration)
	return start.Add(time.Duration(np.Offset) * np.LyrDuration)
}

// Server is used to serve bootstrap update data during systest. NOT intended for production use.
// in particular, it does not protect against data loss and will serve whatever is the latest
// one on disk, even tho it's for an old epoch.
type Server struct {
	*http.Server
	eg          errgroup.Group
	logger      log.Log
	fs          afero.Fs
	gen         *Generator
	genFallback bool
}

func NewServer(fs afero.Fs, gen *Generator, fallback bool, port int, lg log.Log) *Server {
	return &Server{
		Server:      &http.Server{Addr: fmt.Sprintf(":%d", port)},
		logger:      lg,
		fs:          fs,
		gen:         gen,
		genFallback: fallback,
	}
}

func (s *Server) Start(ctx context.Context, errCh chan error, params *NetworkParam) {
	if err := s.fs.MkdirAll(dataDir, 0o700); err != nil {
		errCh <- fmt.Errorf("create persist dir %v: %w", dataDir, err)
	}
	s.eg.Go(func() error {
		s.startHttp(errCh)
		return nil
	})

	if s.gen != nil {
		s.eg.Go(func() error {
			s.loop(ctx, errCh, params)
			return nil
		})
	}
}

func (s *Server) loop(ctx context.Context, errCh chan error, params *NetworkParam) {
	wait := time.Until(params.updateBeaconTime(bootstrapEpoch))
	select {
	case <-time.After(wait):
		if err := s.GenBootstrap(ctx, 2); err != nil {
			errCh <- err
			return
		}
	case <-ctx.Done():
		return
	}

	if !s.genFallback {
		return
	}

	// start generating fallback data
	s.eg.Go(
		func() error {
			s.genDataLoop(ctx, errCh, bootstrapEpoch, params.updateActiveSetTime, s.GenFallbackActiveSet)
			return nil
		})
	s.eg.Go(
		func() error {
			s.genDataLoop(ctx, errCh, bootstrapEpoch+1, params.updateBeaconTime, s.GenFallbackBeacon)
			return nil
		})
}

// in systests, we want to be sure the nodes use the fallback data unconditionally.
// use a fixed known value for beacon to be sure that fallback is used during testing.
func epochBeacon(epoch types.EpochID) types.Beacon {
	b := make([]byte, types.BeaconSize)
	binary.LittleEndian.PutUint32(b, uint32(epoch))
	return types.BytesToBeacon(b)
}

func (s *Server) GenBootstrap(ctx context.Context, epoch types.EpochID) error {
	actives, err := getPartialActiveSet(ctx, epoch, s.gen.SmEndpoint())
	if err != nil {
		return err
	}
	return s.gen.GenUpdate(epoch, epochBeacon(epoch), actives)
}

func (s *Server) GenFallbackBeacon(_ context.Context, epoch types.EpochID) error {
	return s.gen.GenUpdate(epoch, epochBeacon(epoch), nil)
}

func (s *Server) GenFallbackActiveSet(ctx context.Context, epoch types.EpochID) error {
	actives, err := getPartialActiveSet(ctx, epoch, s.gen.SmEndpoint())
	if err != nil {
		return err
	}
	return s.gen.GenUpdate(epoch, types.EmptyBeacon, actives)
}

// in systests, we want to be sure the nodes use the fallback data unconditionally
// we only use half of the active set as fallback value, so we can be sure that fallback is used during testing.
func getPartialActiveSet(ctx context.Context, targetEpoch types.EpochID, smEndpoint string) ([]types.ATXID, error) {
	actives, err := getActiveSet(ctx, smEndpoint, targetEpoch-1)
	if err != nil {
		return nil, err
	}
	// enough to allow hare to pass
	cutoff := len(actives) * 3 / 4
	return actives[:cutoff], nil
}

func (s *Server) genDataLoop(
	ctx context.Context,
	errCh chan error,
	start types.EpochID,
	timeFunc func(types.EpochID) time.Time,
	genFunc func(context.Context, types.EpochID) error,
) {
	for epoch := start; ; epoch++ {
		wait := time.Until(timeFunc(epoch))
		select {
		case <-time.After(wait):
			if err := genFunc(ctx, epoch); err != nil {
				errCh <- err
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) startHttp(ch chan error) {
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		ch <- err
		return
	}
	http.HandleFunc("/", s.handle)
	s.logger.With().Info("server starts serving", log.String("addr", ln.Addr().String()))
	if err = s.Serve(ln); err != nil {
		ch <- err
	}
}

func (s *Server) Stop(ctx context.Context) {
	_ = s.Shutdown(ctx)
	_ = s.eg.Wait()
}

func (s *Server) handle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	data, err := afero.ReadFile(s.fs, PersistedFilename())
	if err != nil && errors.Is(err, afero.ErrFileNotFound) {
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if _, err = w.Write(data); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
