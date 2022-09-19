package peerexchange

import (
	"context"
	"math/rand"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/addressbook"
)

// CheckBook periodically checks the book for dead|alive peers.
func (d *Discovery) CheckBook(ctx context.Context) {
	if d.cfg.CheckInterval == 0 {
		d.logger.Info("periodically peers checking disabled")
		return
	}
	ticker := time.NewTicker(d.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.logger.Debug("checking book")
			d.CheckPeers(ctx)
			d.logger.Debug("checking book done")
		case <-ctx.Done():
			return
		}
	}
}

// CheckPeers periodically checks the book for dead|alive peers.
func (d *Discovery) CheckPeers(ctx context.Context) {
	peers := d.GetRandomPeers(d.cfg.CheckPeersNumber)
	if len(peers) == 0 {
		d.logger.Debug("no peers to check")
		return
	}
	qCtx, cancel := context.WithTimeout(ctx, d.cfg.CheckTimeout)
	defer cancel()
	if _, err := d.crawl.query(qCtx, peers); err != nil {
		d.logger.Error("failed to check nodes: %s", err)
		return
	}
	// check peers are updated in host peerBook.
	for peerID, addresses := range d.peersToMap(peers) {
		if err := d.host.Network().ClosePeer(peerID); err != nil { // close connection, used only for check
			d.logger.Error("failed to close peer after check: %s", err)
		}
		peerStoreAddresses := d.host.Peerstore().Addrs(peerID)
		if len(peerStoreAddresses) == 0 {
			d.logger.With().Debug("Removing address", log.String("peer", peerID.Pretty()))
			d.book.RemoveAddress(peerID)
			continue
		}
		// in case if there are more than one address for peerID is available - for us this is not important, just take first one.
		newAddr, err := addressbook.ParseAddrInfo(peerStoreAddresses[0].String() + "/p2p/" + peerID.Pretty())
		if err != nil {
			d.logger.Error("failed to parse address: %s", err)
			continue
		}
		// update node address in book.
		d.book.AddAddress(newAddr, addresses[0])
	}
}

// GetAddresses returns all addresses of the node.
func (d *Discovery) GetAddresses() []*addressbook.AddrInfo {
	return d.book.GetAddresses()
}

// GetRandomPeers get random N peers from provided peers list.
// peer should satisfy the following conditions:
// - peer is not a bootnode
// - peer is not a connected one
// - peer was attempted to connect X time in ago (defined in config).
func (d *Discovery) GetRandomPeers(n int) []*addressbook.AddrInfo {
	lastUsageDate := time.Now().Add(-1 * d.cfg.CheckPeersUsedBefore)
	allPeers := d.book.GetAddressesNotConnectedSince(lastUsageDate)
	peers := d.filterPeers(allPeers)
	if len(peers) == 0 {
		return nil
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	data := make(map[peer.ID]*addressbook.AddrInfo) // use map in case of duplicates.
	for len(data) < n && len(data) < len(peers) {   // as it random - loop until we get exact number of peers.
		index := r.Intn(len(peers))
		data[peers[index].ID] = peers[index]
	}

	result := make([]*addressbook.AddrInfo, 0, len(data))
	for i := range data {
		addresses := d.host.Peerstore().Addrs(data[i].ID)
		for _, addr := range addresses {
			bookAddr := addr.String() + "/p2p/" + data[i].ID.String()
			if bookAddr == data[i].Addr().String() {
				continue
			}
			pa, err := addressbook.ParseAddrInfo(bookAddr)
			if err != nil {
				d.logger.Error("failed to parse address: %s", err)
				continue
			}
			result = append(result, pa)
		}

		result = append(result, data[i])
	}
	return result
}

func (d *Discovery) filterPeers(peers []*addressbook.AddrInfo) []*addressbook.AddrInfo {
	result := make([]*addressbook.AddrInfo, 0, len(peers))
	for _, p := range peers {
		if d.host.ConnManager().IsProtected(p.ID, BootNodeTag) {
			continue
		}
		if d.host.Network().Connectedness(p.ID) == network.Connected {
			continue
		}
		result = append(result, p)
	}
	return result
}

func (d *Discovery) peersToMap(addresses []*addressbook.AddrInfo) map[peer.ID][]*addressbook.AddrInfo {
	data := make(map[peer.ID][]*addressbook.AddrInfo)
	for _, addr := range addresses {
		data[addr.ID] = append(data[addr.ID], addr)
	}
	return data
}
