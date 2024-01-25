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
	hardTimeout     time.Duration
	totalRead       int
	totalWritten    int
	start           time.Time
	clock           clockwork.Clock
	chunkSize       int
	nextAdjustRead  int
	nextAdjustWrite int
	hardDeadline    time.Time
}

func newDeadlineAdjuster(stream peerStream, desc string, timeout, hardTimeout time.Duration) *deadlineAdjuster {
	return &deadlineAdjuster{
		peerStream:      stream,
		desc:            desc,
		timeout:         timeout,
		hardTimeout:     hardTimeout,
		start:           time.Now(),
		clock:           clockwork.NewRealClock(),
		chunkSize:       deadlineAdjusterChunkSize,
		nextAdjustRead:  -1,
		nextAdjustWrite: -1,
	}
}

func (dadj *deadlineAdjuster) augmentError(what string, err error) error {
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, yamux.ErrTimeout) {
		return err
	}
	now := dadj.clock.Now()
	return fmt.Errorf("%s: %s after %v, %d bytes read, %d bytes written, timeout %v, hard timeout %v: %w",
		dadj.desc,
		what,
		now.Sub(dadj.start),
		dadj.totalRead,
		dadj.totalWritten,
		dadj.timeout,
		dadj.hardTimeout,
		err)
}

func (dadj *deadlineAdjuster) adjust() error {
	now := dadj.clock.Now()
	if dadj.hardDeadline.IsZero() {
		dadj.hardDeadline = now.Add(dadj.hardTimeout)
	} else if now.After(dadj.hardDeadline) {
		// emulate yamux timeout error
		return yamux.ErrTimeout
	}
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
		deadline := now.Add(dadj.timeout)
		if deadline.After(dadj.hardDeadline) {
			_ = dadj.SetDeadline(dadj.hardDeadline)
		} else {
			_ = dadj.SetDeadline(deadline)
		}
	}

	return nil
}

func (dadj *deadlineAdjuster) Read(p []byte) (n int, err error) {
	var nCur int
	for n < len(p) {
		if err := dadj.adjust(); err != nil {
			return n, dadj.augmentError("read", err)
		}
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
		if err := dadj.adjust(); err != nil {
			return n, dadj.augmentError("write", err)
		}
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
