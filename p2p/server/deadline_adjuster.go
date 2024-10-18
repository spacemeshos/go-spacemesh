package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/libp2p/go-yamux/v4"
)

const (
	deadlineAdjusterChunkSize = 4096
)

type deadlineAdjusterError struct {
	what         string
	innerErr     error
	elapsed      time.Duration
	totalRead    int
	totalWritten int
	timeout      time.Duration
	hardTimeout  time.Duration
}

func (err *deadlineAdjusterError) Unwrap() error {
	return err.innerErr
}

func (err *deadlineAdjusterError) Error() string {
	return fmt.Sprintf("%s: %v elapsed, %d bytes read, %d bytes written, timeout %v, hard timeout %v: %v",
		err.what,
		err.elapsed,
		err.totalRead,
		err.totalWritten,
		err.timeout,
		err.hardTimeout,
		err.innerErr)
}

type deadlineAdjuster struct {
	peerStream
	adjustMtx       sync.Mutex
	timeout         time.Duration
	hardTimeout     time.Duration
	totalRead       atomic.Int64
	totalWritten    atomic.Int64
	start           time.Time
	clock           clockwork.Clock
	chunkSize       int
	nextAdjustRead  int
	nextAdjustWrite int
	hardDeadline    time.Time
}

var _ io.ReadWriteCloser = &deadlineAdjuster{}

func newDeadlineAdjuster(stream peerStream, timeout, hardTimeout time.Duration) *deadlineAdjuster {
	return &deadlineAdjuster{
		peerStream:      stream,
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

	return &deadlineAdjusterError{
		what:         what,
		innerErr:     err,
		elapsed:      dadj.clock.Now().Sub(dadj.start),
		totalRead:    int(dadj.totalRead.Load()),
		totalWritten: int(dadj.totalWritten.Load()),
		timeout:      dadj.timeout,
		hardTimeout:  dadj.hardTimeout,
	}
}

// Close closes the stream. This method is safe to call multiple times.
func (dadj *deadlineAdjuster) Close() error {
	// FIXME: unsure if this is really needed (inherited from the older Server code)
	_ = dadj.peerStream.SetDeadline(time.Time{})
	return dadj.peerStream.Close()
}

func (dadj *deadlineAdjuster) adjust() error {
	dadj.adjustMtx.Lock()
	defer dadj.adjustMtx.Unlock()
	now := dadj.clock.Now()
	if dadj.hardDeadline.IsZero() {
		dadj.hardDeadline = now.Add(dadj.hardTimeout)
	} else if now.After(dadj.hardDeadline) {
		// emulate yamux timeout error
		return yamux.ErrTimeout
	}
	// Do not adjust the deadline too often
	adj := false
	if int(dadj.totalRead.Load()) > dadj.nextAdjustRead {
		dadj.nextAdjustRead = int(dadj.totalRead.Load()) + dadj.chunkSize
		adj = true
	}
	if int(dadj.totalWritten.Load()) > dadj.nextAdjustWrite {
		dadj.nextAdjustWrite = int(dadj.totalWritten.Load()) + dadj.chunkSize
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

func (dadj *deadlineAdjuster) Read(p []byte) (int, error) {
	var n int
	for n < len(p) {
		if err := dadj.adjust(); err != nil {
			return n, dadj.augmentError("read", err)
		}
		to := min(len(p), n+dadj.chunkSize)
		nCur, err := dadj.peerStream.Read(p[n:to])
		n += nCur
		dadj.totalRead.Add(int64(nCur))
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
		dadj.totalWritten.Add(int64(nCur))
		if err != nil {
			return n, dadj.augmentError("write", err)
		}
	}
	return n, nil
}
