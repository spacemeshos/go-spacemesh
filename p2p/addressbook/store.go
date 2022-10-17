package addressbook

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	atomicfile "github.com/natefinch/atomic"
	"github.com/pkg/errors"

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
func (a *AddrBook) persistPeers() error {
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
	sam.AnchorAddresses = append(sam.AnchorAddresses, a.anchorPeers...)

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

	data, err := json.Marshal(sam)
	if err != nil {
		return errors.Wrap(err, "failed to encode data")
	}
	if err = a.atomicallySaveToFile(data); err != nil {
		return errors.Wrap(err, "failed to save file")
	}
	return nil
}

func (a *AddrBook) atomicallySaveToFile(data []byte) error {
	checkSum := crc32.Checksum(data, crc32.MakeTable(crc32.IEEE))
	checkSumBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(checkSumBytes, checkSum)
	resultData := append(checkSumBytes, data...)

	if err := atomicfile.WriteFile(a.path, bytes.NewReader(resultData)); err != nil {
		return errors.Wrap(err, "failed to write addresses to file")
	}
	return nil
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
		a.logger.With().Error("failed to parse file", log.String("path", path), log.Err(err))
		// if it is invalid we nuke the old one unconditionally.
		if err = os.Remove(path); err != nil {
			a.logger.With().Warning("failed to remove corrupt peers file", log.String("path", path), log.Err(err))
		}
		a.reset()
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

	data, err := io.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "error reading file")
	}

	if len(data) < 4 {
		return errors.New("file is too short")
	}
	fileCrc := binary.LittleEndian.Uint32(data[:4])
	dataCrc := crc32.Checksum(data[4:], crc32.MakeTable(crc32.IEEE))
	if fileCrc != dataCrc {
		return fmt.Errorf("checksum mismatch: %x != %x", fileCrc, dataCrc)
	}

	var sam serializedAddrManager
	if err = json.Unmarshal(data[4:], &sam); err != nil {
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

	a.lastAnchorPeers = append(a.lastAnchorPeers, sam.AnchorAddresses...)

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
	var err error
	defer ticker.Stop()
	if len(a.path) == 0 {
		return
	}

	for {
		select {
		case <-ticker.C:
			if err = a.persistPeers(); err != nil {
				a.logger.Error("Failed to persist file %s: %v", a.path, err)
			} else {
				a.logger.Debug("saved peers to file %v", a.path)
			}
		case <-ctx.Done():
			a.logger.Debug("saving peer before exit to file %v", a.path)
			if err = a.persistPeers(); err != nil {
				a.logger.Error("Failed to persist file %s: %v", a.path, err)
			}
			return
		}
	}
}
