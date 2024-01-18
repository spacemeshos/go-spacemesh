package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/libp2p/go-yamux/v4"
)

const (
	deadlineAdjusterChunkSize = 4096
)

type deadlineAdjuster struct {
	peerStream
	desc            string
	timeout         time.Duration
	totalRead       int
	totalWritten    int
	start           time.Time
	clock           clockwork.Clock
	chunkSize       int
	nextAdjustRead  int
	nextAdjustWrite int
}

func newDeadlineAdjuster(stream peerStream, desc string, timeout time.Duration) *deadlineAdjuster {
	return &deadlineAdjuster{
		peerStream: stream,
		desc:       desc,
		timeout:    timeout,
		start:      time.Now(),
		clock:      clockwork.NewRealClock(),
		chunkSize:  deadlineAdjusterChunkSize,
	}
}

func (dadj *deadlineAdjuster) augmentError(what string, err error) error {
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, yamux.ErrTimeout) {
		return err
	}
	now := dadj.clock.Now()
	return fmt.Errorf("%s: %s after %v, %d bytes read, %d bytes written, timeout %v: %w",
		dadj.desc,
		what,
		now.Sub(dadj.start),
		dadj.totalRead,
		dadj.totalWritten,
		dadj.timeout,
		err)
}

func (dadj *deadlineAdjuster) adjust() {
	// Do not adjust the deadline too often
	adj := false
	if dadj.totalRead > dadj.nextAdjustRead {
		dadj.nextAdjustRead = dadj.totalRead + dadj.chunkSize
		adj = true
	}
	if dadj.totalWritten > dadj.nextAdjustWrite {
		dadj.nextAdjustWrite = dadj.totalWritten + dadj.chunkSize
		adj = true
	}
	if adj {
		// We ignore the error returned by SetDeadline b/c the call
		// doesn't work for mock hosts
		_ = dadj.SetDeadline(dadj.clock.Now().Add(dadj.timeout))
	}
}

func (dadj *deadlineAdjuster) Read(p []byte) (n int, err error) {
	var nCur int
	for n < len(p) {
		dadj.adjust()
		to := min(len(p), n+dadj.chunkSize)
		nCur, err = dadj.peerStream.Read(p[n:to])
		n += nCur
		dadj.totalRead += nCur
		if err != nil {
			return n, dadj.augmentError("read", err)
		}
		if n < to {
			// Short read, don't try to read more data
			break
		}
	}
	return n, nil
}

func (dadj *deadlineAdjuster) Write(p []byte) (n int, err error) {
	var nCur int
	for n < len(p) {
		dadj.adjust()
		to := min(len(p), n+dadj.chunkSize)
		nCur, err = dadj.peerStream.Write(p[n:to])
		n += nCur
		dadj.totalWritten += nCur
		if err != nil {
			return n, dadj.augmentError("write", err)
		}
	}
	return n, nil
}
