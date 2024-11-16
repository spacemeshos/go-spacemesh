package server

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

type dumpDir int

const (
	dumpDirNone = iota
	dumpDirLocalToRemote
	dumpDirRemoteToLocal
)

type dumpItem struct {
	dir  dumpDir
	data []byte
}

type dumpStream struct {
	peerStream
	maxIdle  time.Duration
	setup    sync.Once
	eg       errgroup.Group
	ch       chan dumpItem
	out      io.WriteCloser
	curDir   dumpDir
	acc      []byte
	mtx      sync.Mutex
	prevTime time.Time
	started  bool
}

func newDumpStream(s peerStream, proto, remote, outDir string, maxIdle time.Duration) peerStream {
	proto = strings.ReplaceAll(proto, "/", "-")
	remote = strings.ReplaceAll(remote, "/", "-")
	outDir = filepath.Join(outDir, proto)
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return s
	}
	outPath := filepath.Join(outDir, fmt.Sprintf("%s.txt", remote))
	out, err := os.OpenFile(outPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return s
	}
	return &dumpStream{
		peerStream: s,
		maxIdle:    maxIdle,
		ch:         make(chan dumpItem),
		curDir:     dumpDirNone,
		out:        out,
	}
}

func (ds *dumpStream) flush() {
	if len(ds.acc) == 0 {
		ds.curDir = dumpDirNone
		return
	}

	var dir, afterStr string
	switch ds.curDir {
	case dumpDirNone:
		return
	case dumpDirLocalToRemote:
		dir = "local -> remote"
	case dumpDirRemoteToLocal:
		dir = "remote -> local"
	}
	now := time.Now().UTC()
	if !ds.started {
		ds.started = true
		fmt.Fprintf(ds.out, "*** BEGIN @ %s ***\n\n", now.Format(time.RFC3339Nano))
	}
	if !ds.prevTime.IsZero() {
		afterStr = fmt.Sprintf(" (after %v)", now.Sub(ds.prevTime))
	}
	fmt.Fprintf(ds.out, "--- %s%s: %s ---\n%s\n\n",
		now.Format(time.RFC3339Nano), afterStr, dir, hex.Dump(ds.acc))
	ds.prevTime = now
	ds.acc = ds.acc[:0]
	ds.curDir = dumpDirNone
}

func (ds *dumpStream) begin() {
	ds.setup.Do(func() {
		if ds.out == nil {
			return
		}
		ds.eg.Go(func() error {
			for {
				select {
				// TBD: hung stream: dump goroutines
				case <-time.After(ds.maxIdle):
					ds.flush()
				case item, ok := <-ds.ch:
					if !ok {
						ds.flush()
						return nil
					}
					if ds.curDir != item.dir {
						ds.flush()
						ds.curDir = item.dir
					}
					ds.acc = append(ds.acc, item.data...)
				}
			}
		})
	})
}

func (ds *dumpStream) Close() error {
	ds.mtx.Lock()
	if ds.ch != nil {
		close(ds.ch)
		ds.eg.Wait()
		ds.ch = nil
	}
	ds.mtx.Unlock()
	return ds.peerStream.Close()
}

func (ds *dumpStream) active() bool {
	if ds.out == nil {
		return false
	}
	ds.mtx.Lock()
	defer ds.mtx.Unlock()
	return ds.ch != nil
}

func (ds *dumpStream) Read(d []byte) (n int, err error) {
	ds.begin()
	n, err = ds.peerStream.Read(d)
	if n > 0 && ds.active() {
		ds.ch <- dumpItem{
			dir:  dumpDirRemoteToLocal,
			data: slices.Clone(d[:n]),
		}
	}
	return
}

func (ds *dumpStream) Write(d []byte) (n int, err error) {
	ds.begin()
	n, err = ds.peerStream.Write(d)
	if n > 0 && ds.active() {
		ds.ch <- dumpItem{
			dir:  dumpDirLocalToRemote,
			data: slices.Clone(d[:n]),
		}
	}
	return
}
