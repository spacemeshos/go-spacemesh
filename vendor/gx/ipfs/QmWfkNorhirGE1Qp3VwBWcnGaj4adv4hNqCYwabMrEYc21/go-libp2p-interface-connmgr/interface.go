package ifconnmgr

import (
	"context"
	"time"

	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	inet "gx/ipfs/QmbD5yKbXahNvoMqzeuNyKQA9vAs9fUvJg2GXeWU1fVqY5/go-libp2p-net"
)

type ConnManager interface {
	TagPeer(peer.ID, string, int)
	UntagPeer(peer.ID, string)
	GetTagInfo(peer.ID) *TagInfo
	TrimOpenConns(context.Context)
	Notifee() inet.Notifiee
}

type TagInfo struct {
	FirstSeen time.Time
	Value     int
	Tags      map[string]int
	Conns     map[string]time.Time
}
