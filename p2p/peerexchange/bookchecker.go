package peerexchange

import (
	"context"
	"math/rand"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
)

const (
	checkInterval = 3 * time.Minute  // Interval to check for dead|alive peers in the book.
	checkTimeout  = 30 * time.Second // Timeout to check for dead|alive peers in the book.
	peersNumber   = 10               // Number of peers to check for dead|alive peers in the book.
)

// CheckBook periodically checks the book for dead|alive peers.
func (d *Discovery) CheckBook(ctx context.Context) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.logger.Debug("checking book")
			d.checkPeers()
			d.logger.Debug("checking book done")
		case <-ctx.Done():
			return
		}
	}
}

func (d *Discovery) checkPeers() {
	peers := getRandomPeers(d.book.AddressCache(), peersNumber)
	now := time.Now()
	qCtx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	if _, err := d.crawl.query(qCtx, peers); err != nil {
		d.logger.Error("failed to query bootstrap node: %s", err)
		return
	}
	// check peers are updated in book.
	for _, p := range peers {
		bPeer := d.book.lookup(p.ID)
		if bPeer.LastSuccess.After(now) {
			continue // peer marked as good, it's alive
		}
		d.book.RemoveAddress(p.ID)
	}
}

// getRandomPeers get random N peers from provided peers list.
func getRandomPeers(peers []*addrInfo, n int) []*addrInfo {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	data := make(map[peer.ID]*addrInfo, n) // use map in case of duplicates.
	for i := 0; i < n; i++ {
		index := r.Intn(len(peers))
		data[peers[r.Intn(len(peers))].ID] = peers[index]
	}

	result := make([]*addrInfo, 0, len(data))
	for i, _ := range data {
		result = append(result, data[i])
	}
	return result
}
