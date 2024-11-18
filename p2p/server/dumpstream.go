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

type dumpOp int

const (
	dumpOpNone = iota
	dumpOpLocalToRemote
	dumpOpRemoteToLocal
	dumpOpStartLocalToRemote
	dumpOpStartRemoteToLocal
)

func (dk dumpOp) blocking() bool {
	switch dk {
	case dumpOpStartLocalToRemote, dumpOpStartRemoteToLocal:
		return true
	default:
		return false
	}
}

func (dk dumpOp) dir() dumpDir {
	switch dk {
	case dumpOpStartLocalToRemote, dumpOpLocalToRemote:
		return dumpDirLocalToRemote
	case dumpOpStartRemoteToLocal, dumpOpRemoteToLocal:
		return dumpDirRemoteToLocal
	default:
		return dumpOpNone
	}
}

type dumpItem struct {
	op    dumpOp
	count int
	data  []byte
	ts    time.Time
	error error
}

type dumpStream struct {
	peerStream
	maxIdle  time.Duration
	setup    sync.Once
	eg       errgroup.Group
	ch       chan dumpItem
	out      io.WriteCloser
	curDir   dumpDir
	acc      []dumpItem
	mtx      sync.Mutex
	started  bool
	prevTime time.Time
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
		curDir:     dumpOpNone,
		out:        out,
	}
}

func (ds *dumpStream) flush() {
	if len(ds.acc) == 0 {
		ds.curDir = dumpOpNone
		return
	}

	var dir, afterStr string
	switch ds.curDir {
	case dumpOpNone:
		return
	case dumpOpLocalToRemote:
		dir = "local -> remote (send)"
	case dumpOpRemoteToLocal:
		dir = "remote -> local (recv)"
	}
	startTS := ds.acc[0].ts
	if !ds.started {
		ds.started = true
		fmt.Fprintf(ds.out, "*** BEGIN @ %s ***\n\n", startTS.Format(time.RFC3339Nano))
	}
	var d []byte
	for _, item := range ds.acc {
		d = append(d, item.data...)
	}
	if !ds.prevTime.IsZero() {
		afterStr = fmt.Sprintf(" (after %v)", startTS.Sub(ds.prevTime))
	}
	if len(d) > 0 {
		fmt.Fprintf(ds.out, "--- %s%s: %s ---\n%s\n\n",
			startTS.Format(time.RFC3339Nano), afterStr, dir, hex.Dump(d))
	}
	last := ds.acc[len(ds.acc)-1]
	ds.prevTime = last.ts
	if last.op.blocking() {
		fmt.Fprintf(ds.out, "--- %s: %s: blocked for %v ---\n\n",
			startTS.Format(time.RFC3339Nano), dir,
			time.Now().Sub(last.ts))
	}
	for _, item := range ds.acc {
		if item.error != nil {
			fmt.Fprintf(ds.out, "Error: %v\n\n", item.error)
			break
		}
	}
	ds.acc = ds.acc[:0]
	ds.curDir = dumpOpNone
}

func (ds *dumpStream) handleDumpItem(item dumpItem) {
	dir := item.op.dir()
	if ds.curDir != dir {
		ds.flush()
		ds.curDir = dir
	}
	ds.acc = append(ds.acc, item)
	if item.error != nil {
		ds.flush()
	}
}

func (ds *dumpStream) begin() {
	ds.setup.Do(func() {
		if ds.out == nil {
			return
		}
		ds.eg.Go(func() error {
			for {
				select {
				case <-time.After(ds.maxIdle):
					ds.flush()
				case item, ok := <-ds.ch:
					if !ok {
						ds.flush()
						return nil
					}
					ds.handleDumpItem(item)
				}
			}
		})
	})
}

func (ds *dumpStream) active() bool {
	if ds.out == nil {
		return false
	}
	ds.mtx.Lock()
	defer ds.mtx.Unlock()
	return ds.ch != nil
}

func (ds *dumpStream) toDump(op dumpOp, count int, data []byte) {
	if ds.active() {
		ds.ch <- dumpItem{
			op:    op,
			count: count,
			data:  slices.Clone(data),
			ts:    time.Now(),
		}
	}
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

func (ds *dumpStream) Read(d []byte) (n int, err error) {
	ds.begin()
	ds.toDump(dumpOpStartRemoteToLocal, len(d), nil)
	n, err = ds.peerStream.Read(d)
	ds.toDump(dumpOpRemoteToLocal, n, d[:n])
	return n, err
}

func (ds *dumpStream) Write(d []byte) (n int, err error) {
	ds.begin()
	ds.toDump(dumpOpStartLocalToRemote, len(d), nil)
	n, err = ds.peerStream.Write(d)
	ds.toDump(dumpOpLocalToRemote, n, d[:n])
	return n, err
}

// TBD: QQQQQ: if there are no pending read/write ops for a while, dump all goroutine stacks
