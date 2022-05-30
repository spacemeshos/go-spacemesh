package addressbook

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	peersFileName   = "peers.json"
	persistInterval = 10 * time.Minute
)

type serializedAddrManager struct {
	Key             [32]byte
	AnchorAddresses []*knownAddress
	Addresses       []*knownAddress
	NewBuckets      [][]peer.ID
	TriedBuckets    [][]peer.ID
}

// persistPeers saves all the known addresses to a file so they can be read back in at next run.
func (a *AddrBook) persistPeers(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	sam := new(serializedAddrManager)
	copy(sam.Key[:], a.key[:])

	sam.Addresses = make([]*knownAddress, 0, len(a.addrIndex))
	sam.AnchorAddresses = make([]*knownAddress, 0, len(a.addrIndex))
	sam.NewBuckets = make([][]peer.ID, a.cfg.NewBucketCount)
	sam.TriedBuckets = make([][]peer.ID, a.cfg.TriedBucketCount)

	for _, addr := range a.addrIndex {
		sam.Addresses = append(sam.Addresses, addr)
	}
	for i := range a.addrNew {
		sam.NewBuckets[i] = make([]peer.ID, len(a.addrNew[i]))
		j := 0
		for _, v := range a.addrNew[i] {
			sam.NewBuckets[i][j] = v.Addr.ID
			j++
		}
	}
	for i := range a.addrTried {
		sam.TriedBuckets[i] = make([]peer.ID, len(a.addrTried[i]))
		j := 0
		for _, v := range a.addrTried[i] {
			sam.TriedBuckets[i][j] = v.Addr.ID
			j++
		}
	}

	w, err := os.Create(path)
	if err != nil {
		a.logger.LogError("Error creating file", err)
		return
	}
	enc := json.NewEncoder(w)
	defer w.Close()
	if err := enc.Encode(&sam); err != nil {
		a.logger.LogError("Failed to encode file", err, log.String("path", path))
		return
	}
}

// loadPeers loads the known address from the saved file. If empty, missing, or
// malformed file, just don't load anything and start fresh.
func (a *AddrBook) loadPeers(path string) {
	if len(path) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	// we don't lock the mutex in decodeFrom because it might fail and we'll run reset
	if err := a.decodeFrom(path); err != nil {
		a.logger.With().LogError("failed to parse file", err, log.String("path", path))
		// if it is invalid we nuke the old one unconditionally.
		if err = os.Remove(path); err != nil {
			a.logger.With().Warning("failed to remove corrupt peers file", log.String("path", path), log.Err(err))
		}
		a.reset()
		return
	}
}

func (a *AddrBook) decodeFrom(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		a.logger.With().Debug("peers not loaded to addrbook since file does not exist",
			log.String("path", path))
		return nil
	}
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}
	defer r.Close()

	var sam serializedAddrManager
	if err = json.NewDecoder(r).Decode(&sam); err != nil {
		return fmt.Errorf("error reading %s: %w", path, err)
	}

	copy(a.key[:], sam.Key[:])

	for _, v := range sam.Addresses {
		a.addrIndex[v.Addr.ID] = v
	}

	for i := range sam.NewBuckets {
		for _, pid := range sam.NewBuckets[i] {
			ka, ok := a.addrIndex[pid]
			if !ok {
				return fmt.Errorf("newbucket contains %s but none in address list", pid)
			}

			if ka.refs == 0 {
				a.nNew++
			}
			ka.refs++
			a.addrNew[i][pid] = ka
		}
	}

	for i := range sam.TriedBuckets {
		for _, pid := range sam.TriedBuckets[i] {
			ka, ok := a.addrIndex[pid]
			if !ok {
				return fmt.Errorf("tried bucket contains %s but none in address list", pid)
			}

			if ka.refs == 0 {
				a.nTried++
			}
			ka.refs++
			a.addrTried[i][pid] = ka
		}
	}

	for _, v := range sam.AnchorAddresses {
		a.lastAnchorPeers = append(a.lastAnchorPeers, v)
	}

	// Sanity checking.
	for k, v := range a.addrIndex {
		if v.refs == 0 && !v.tried {
			return fmt.Errorf("address %s after serialization with no references", k)
		}

		if v.refs > 0 && v.tried {
			return fmt.Errorf("address %s after serialization which is both new and tried! ", k)
		}
	}

	a.logger.Info("Loaded %d addresses from file '%s'", a.numAddresses(), path)
	return nil
}

// Persist runs a loop that periodically persists address book on disk.
// If started with canceled context it will persist exactly once.
func (a *AddrBook) Persist(ctx context.Context) {
	ticker := time.NewTicker(persistInterval)
	defer ticker.Stop()
	path := a.path
	if len(path) == 0 {
		return
	}

	for {
		select {
		case <-ticker.C:
			a.persistPeers(path)
			a.logger.Debug("saved peers to file %v", path)
		case <-ctx.Done():
			a.logger.Debug("saving peer before exit to file %v", path)
			a.persistPeers(path)
			return
		}
	}
}
