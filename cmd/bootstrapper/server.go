package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const fileRegex = "/epoch-(?P<Epoch>[0-9]+)-update-(?P<Suffix>[a-z]+)"

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
	eg              errgroup.Group
	logger          log.Log
	fs              afero.Fs
	gen             *Generator
	genFallback     bool
	bootstrapEpochs []types.EpochID
	regex           *regexp.Regexp
}

type SrvOpt func(*Server)

func WithSrvLogger(logger log.Log) SrvOpt {
	return func(s *Server) {
		s.logger = logger
	}
}

func WithSrvFilesystem(fs afero.Fs) SrvOpt {
	return func(s *Server) {
		s.fs = fs
	}
}

func WithBootstrapEpochs(epochs []types.EpochID) SrvOpt {
	return func(s *Server) {
		s.bootstrapEpochs = epochs
	}
}

func NewServer(gen *Generator, fallback bool, port int, opts ...SrvOpt) *Server {
	s := &Server{
		Server:          &http.Server{Addr: fmt.Sprintf(":%d", port)},
		logger:          log.NewNop(),
		fs:              afero.NewOsFs(),
		gen:             gen,
		genFallback:     fallback,
		bootstrapEpochs: []types.EpochID{2},
		regex:           regexp.MustCompile(fileRegex),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Server) Start(ctx context.Context, errCh chan error, params *NetworkParam) {
	if err := s.fs.MkdirAll(dataDir, 0o700); err != nil {
		errCh <- fmt.Errorf("create persist dir %v: %w", dataDir, err)
	}
	s.eg.Go(func() error {
		ln, err := net.Listen("tcp", s.Addr)
		if err != nil {
			errCh <- err
			return err
		}
		http.HandleFunc("/", s.handle)
		http.HandleFunc("/checkpoint", s.handleCheckpoint)
		http.HandleFunc("/updateCheckpoint", s.handleUpdate)
		s.logger.With().Info("server starts serving", log.String("addr", ln.Addr().String()))
		if err = s.Serve(ln); err != nil {
			errCh <- err
			return err
		}

		return nil
	})

	s.eg.Go(func() error {
		var last types.EpochID
		for _, epoch := range s.bootstrapEpochs {
			wait := time.Until(params.updateBeaconTime(epoch))
			select {
			case <-time.After(wait):
				if err := s.GenBootstrap(ctx, epoch); err != nil {
					errCh <- err
					return err
				}
				last = epoch
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if !s.genFallback {
			return nil
		}

		// start generating fallback data
		s.eg.Go(func() error {
			for epoch := last; ; epoch++ {
				wait := time.Until(params.updateActiveSetTime(epoch))
				select {
				case <-time.After(wait):
					if err := s.genWithRetry(ctx, epoch, 10); err != nil {
						errCh <- err
						return nil
					}
				case <-ctx.Done():
					return nil
				}
			}
		})
		s.eg.Go(func() error {
			for epoch := last + 1; ; epoch++ {
				wait := time.Until(params.updateBeaconTime(epoch))
				select {
				case <-time.After(wait):
					if err := s.GenFallbackBeacon(epoch); err != nil {
						errCh <- err
						return err
					}
				case <-ctx.Done():
					return nil
				}
			}
		})

		return nil
	})
}

func (s *Server) genWithRetry(ctx context.Context, epoch types.EpochID, maxRetries int) error {
	err := s.GenFallbackActiveSet(ctx, epoch)
	if err == nil {
		return nil
	}
	s.logger.With().Debug("generate fallback active set retry", log.Err(err))

	retries := 0
	backoff := 10 * time.Second
	timer := time.NewTimer(backoff)

	for {
		select {
		case <-timer.C:
			if err := s.GenFallbackActiveSet(ctx, epoch); err != nil {
				s.logger.With().Debug("generate fallback active set retry", log.Err(err))
				retries++
				if retries >= maxRetries {
					return err
				}
				timer.Reset(backoff)
				continue
			}
			return nil
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		}
	}
}

// in systests, we want to be sure the nodes use the fallback data unconditionally.
// use a fixed known value for beacon to be sure that fallback is used during testing.
func epochBeacon(epoch types.EpochID) types.Beacon {
	b := make([]byte, types.BeaconSize)
	binary.LittleEndian.PutUint32(b, uint32(epoch))
	return types.BytesToBeacon(b)
}

func (s *Server) GenBootstrap(ctx context.Context, epoch types.EpochID) error {
	actives, err := getActiveSet(ctx, s.gen.SmEndpoint(), epoch-1)
	if err != nil {
		return err
	}
	suffix := bootstrap.SuffixBootstrap
	_, err = s.gen.GenUpdate(epoch, epochBeacon(epoch), actives, suffix)
	return err
}

func (s *Server) GenFallbackBeacon(epoch types.EpochID) error {
	suffix := bootstrap.SuffixBeacon
	_, err := s.gen.GenUpdate(epoch, epochBeacon(epoch), nil, suffix)
	return err
}

func (s *Server) GenFallbackActiveSet(ctx context.Context, epoch types.EpochID) error {
	suffix := bootstrap.SuffixActiveSet
	actives, err := getPartialActiveSet(ctx, s.gen.SmEndpoint(), epoch)
	if err != nil {
		return err
	}
	_, err = s.gen.GenUpdate(epoch, types.EmptyBeacon, actives, suffix)
	return err
}

// in systests, we want to be sure the nodes use the fallback data unconditionally
// we only use half of the active set as fallback value, so we can be sure that fallback is used during testing.
func getPartialActiveSet(ctx context.Context, smEndpoint string, targetEpoch types.EpochID) ([]types.ATXID, error) {
	actives, err := getActiveSet(ctx, smEndpoint, targetEpoch-1)
	if err != nil {
		return nil, err
	}
	// enough to allow hare to pass
	cutoff := len(actives) * 3 / 4
	return actives[:cutoff], nil
}

func (s *Server) Stop(ctx context.Context) {
	s.logger.With().Info("shutting down server")
	s.Shutdown(ctx)
	s.eg.Wait()
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	matches := s.regex.FindStringSubmatch(r.URL.String())
	if len(matches) != 3 {
		s.logger.With().Error("unrecognized url", log.String("url", r.URL.String()))
		w.WriteHeader(http.StatusNotFound)
		return
	}
	e, err := strconv.Atoi(matches[1])
	if err != nil {
		s.logger.With().Error("unrecognized url", log.String("url", r.URL.String()), log.Err(err))
		w.WriteHeader(http.StatusNotFound)
		return
	}
	epoch := types.EpochID(e)
	suffix := matches[2]
	serveFile := PersistedFilename(epoch, suffix)
	s.servefile(serveFile, w)
}

func (s *Server) handleCheckpoint(w http.ResponseWriter, _ *http.Request) {
	s.servefile(CheckpointFilename(), w)
}

func (s *Server) servefile(f string, w http.ResponseWriter) {
	data, err := afero.ReadFile(s.fs, f)
	if err != nil && errors.Is(err, afero.ErrFileNotFound) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "servefile %s: %v", f, err)
		return
	}

	if _, err = w.Write(data); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "write response %s: %v", f, err)
		return
	}
}

func CheckpointFilename() string {
	return filepath.Join(dataDir, "spacemesh-checkpoint")
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "ParseForm err: %v", err)
		return
	}
	data := r.FormValue("checkpoint")
	filename := CheckpointFilename()
	err := afero.WriteFile(s.fs, filename, []byte(data), 0o600)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "save checkpoint err: %v", err)
		return
	}
	s.logger.With().Info("saved checkpoint data",
		log.String("data", data),
		log.String("filename", filename),
	)
}
