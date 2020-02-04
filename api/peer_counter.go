package api

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

type PeerCounter struct {
	cnt uint64
}

func NewPeerCounter(conn, disc chan p2pcrypto.PublicKey) *PeerCounter {
	p := &PeerCounter{}
	go p.loop(conn, disc)
	return p
}

func (p *PeerCounter) PeerCount() uint64 {
	return p.cnt
}

func (p *PeerCounter) loop(conn, disc chan p2pcrypto.PublicKey) {
	for {
		select {
		case _, ok := <-conn:
			if !ok {
				log.Info("quiting peer counter loop")
				return
			}
			p.cnt++
		case _, ok := <-disc:
			if !ok {
				log.Info("quiting peer counter loop")
				return
			}
			if p.cnt == 0 {
				log.Error("peer counter got more disconnections than connections")
				continue
			}
			p.cnt--
		}
	}
}
