package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"sync"

	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
)

// Server is used to serve bootstrap update data during systest. NOT intended for production use.
// in particular, it does not protect against data loss and will serve whatever is the latest
// one on disk, even tho it's for an old epoch.
type Server struct {
	*http.Server
	eg     errgroup.Group
	logger log.Log
	fs     afero.Fs

	mu         sync.Mutex
	latestFile string
	latestData []byte
}

func NewServer(fs afero.Fs, port int, lg log.Log) *Server {
	return &Server{
		Server: &http.Server{Addr: fmt.Sprintf(":%d", port)},
		logger: lg,
		fs:     fs,
	}
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	s.eg.Go(func() error {
		http.HandleFunc("/", s.handle)
		s.logger.With().Info("server starts serving", log.String("addr", ln.Addr().String()))
		if err = s.Serve(ln); err != nil {
			return err
		}
		return nil
	})
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if err := s.Shutdown(ctx); err != nil {
		return err
	}
	_ = s.eg.Wait()
	return nil
}

func (s *Server) handle(w http.ResponseWriter, _ *http.Request) {
	latest, err := latestFile(s.fs)
	if err != nil {
		s.logger.With().Warning("failed to read latest file", log.Err(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if latest == "" { // no update available
		return
	}
	data := s.cached(latest)
	cached := len(data) > 0
	if !cached {
		data, err = afero.ReadFile(s.fs, latest)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	if _, err = w.Write(data); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !cached {
		s.updateCache(latest, data)
	}
}

func (s *Server) updateCache(latest string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latestFile = latest
	s.latestData = data
}

func (s *Server) cached(latest string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latestFile == latest {
		return s.latestData
	}
	return []byte{}
}

func latestFile(fs afero.Fs) (string, error) {
	dir := persistDir()
	files, err := afero.ReadDir(fs, dir)
	if err != nil {
		return "", fmt.Errorf("list update files: %w", err)
	}
	if len(files) == 0 {
		return "", nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() > files[j].Name() })
	return filepath.Join(dir, files[0].Name()), nil
}
