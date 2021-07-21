package peersync

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

const (
	DefaultMaxErrorCount = 10
)

var (
	ErrPeersNotSynced = errors.New("timesync: peers are not time synced")
)

type Time interface {
	Now() time.Time
}

type systemTime struct{}

func (s systemTime) Now() time.Time {
	return time.Now()
}

type MessageServer interface {
	SendRequest(context.Context, server.MessageType, []byte, peers.Peer, func([]byte), func(error)) error
	RegisterBytesMsgHandler(server.MessageType, func(ctx context.Context, b []byte) []byte)
}

type Request struct {
	ID uint64
}

type DirectRequest struct {
	Peer peers.Peer
	Request
}

type Response struct {
	ID               uint64
	ReceiveTimestamp uint64
	SendTimestamp    uint64
}

type SyncOption func(*Sync)

func WithTime(t Time) SyncOption {
	return func(s *Sync) {
		s.time = t
	}
}

func WithContext(ctx context.Context) SyncOption {
	return func(s *Sync) {
		s.ctx = ctx
	}
}

func WithLog(lg log.Log) SyncOption {
	return func(s *Sync) {
		s.log = lg
	}
}

func New(srv MessageServer, opts ...SyncOption) *Sync {
	sync := &Sync{
		log:       log.NewNop(),
		ctx:       context.Background(),
		time:      systemTime{},
		maxErrCnt: DefaultMaxErrorCount,
	}
	for _, opt := range opts {
		opt(sync)
	}
	sync.ctx, sync.cancel = context.WithCancel(sync.ctx)
	srv.RegisterBytesMsgHandler(server.RequestTimeSync, sync.requestHandler)
	return sync
}

type Sync struct {
	errCnt         uint32
	maxErrCnt      uint32
	log            log.Log
	maxClockOffset time.Duration

	wg     sync.WaitGroup
	once   sync.Once
	srv    MessageServer
	ctx    context.Context
	cancel func()
	errors chan error
	time   Time
}

func (s *Sync) requestHandler(ctx context.Context, buf []byte) []byte {
	var request Request
	if err := types.BytesToInterface(buf, &request); err != nil {
		return nil
	}
	resp := Response{
		ID:               request.ID,
		ReceiveTimestamp: uint64(time.Now().UnixNano()),
		SendTimestamp:    uint64(time.Now().UnixNano()),
	}
	buf, err := types.InterfaceToBytes(&resp)
	if err != nil {
		s.log.Panic("failed to encode timesync response", log.Binary("response", buf), log.Err(err))
	}
	return buf
}
