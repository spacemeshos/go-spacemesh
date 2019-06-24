package discovery

import (
	"encoding/json"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"os"
	"time"
)

const defaultPeersFileName = "peers.json"
const saveRoutineInterval = time.Minute * 10

type serializedKnownAddress struct {
	Addr        string
	Src         string
	Attempts    int
	LastSeen    int64
	LastAttempt int64
	LastSuccess int64
	// no refcount or tried, that is available from context.
}

type serializedAddrManager struct {
	Key          [32]byte
	Addresses    []*serializedKnownAddress
	NewBuckets   [newBucketCount][]string // NodeInfo represented as string
	TriedBuckets [triedBucketCount][]string
}

// savePeers saves all the known addresses to a file so they can be read back
// in at next run.
func (a *addrBook) savePeers(path string) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	// First we make a serialisable datastructure so we can encode it to
	// json.
	sam := new(serializedAddrManager)
	copy(sam.Key[:], a.key[:])

	sam.Addresses = make([]*serializedKnownAddress, len(a.addrIndex))
	i := 0
	for _, v := range a.addrIndex {
		ska := new(serializedKnownAddress)
		ska.Addr = v.na.String()
		ska.Src = v.srcAddr.String()
		ska.LastSeen = v.lastSeen.Unix()
		ska.Attempts = v.attempts
		ska.LastAttempt = v.lastattempt.Unix()
		ska.LastSuccess = v.lastsuccess.Unix()
		// Tried and refs are implicit in the rest of the structure
		// and will be worked out from context on unserialisation.
		sam.Addresses[i] = ska
		i++
	}
	for i := range a.addrNew {
		sam.NewBuckets[i] = make([]string, len(a.addrNew[i]))
		j := 0
		for k := range a.addrNew[i] {
			sam.NewBuckets[i][j] = k
			j++
		}
	}
	for i := range a.addrTried {
		sam.TriedBuckets[i] = make([]string, len(a.addrTried[i]))
		j := 0
		for k := range a.addrTried[i] {
			sam.TriedBuckets[i][j] = k
			j++
		}
	}

	w, err := os.Create(path)
	if err != nil {
		log.Error("Error opening file: %v", err)
		return
	}
	enc := json.NewEncoder(w)
	defer w.Close()
	if err := enc.Encode(&sam); err != nil {
		log.Error("Failed to encode file %s: %v", path, err)
		return
	}
}

// loadPeers loads the known address from the saved file.  If empty, missing, or
// malformed file, just don't load anything and start fresh
func (a *addrBook) loadPeers(filePath string) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	// we don't lock the mutex in deserializePeers because it might fail and we'll run reset

	err := a.deserializePeers(filePath)
	if err != nil {
		log.Error("Failed to parse file %s: %v", filePath, err)
		// if it is invalid we nuke the old one unconditionally.
		err = os.Remove(filePath)
		if err != nil {
			log.Warning("Failed to remove corrupt peers file %s: %v",
				filePath, err)
		}
		a.reset()
		return
	}
}

func (a *addrBook) deserializePeers(filePath string) error {

	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		a.logger.Warning("Peers not loaded to addrbook since file does not exist. file=%v", filePath)
		return nil
	}
	r, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("error opening file: %v", err)
	}
	defer r.Close()

	var sam serializedAddrManager
	dec := json.NewDecoder(r)
	err = dec.Decode(&sam)
	if err != nil {
		return fmt.Errorf("error reading %s: %v", filePath, err)
	}

	copy(a.key[:], sam.Key[:])

	for _, v := range sam.Addresses {
		ka := new(KnownAddress)

		ka.na, err = node.ParseNode(v.Addr)
		if err != nil {
			return fmt.Errorf("failed to deserialize netaddress "+
				"%s: %v", v.Addr, err)
		}

		ka.srcAddr, err = node.ParseNode(v.Src)
		if err != nil {
			return fmt.Errorf("failed to deserialize netaddress "+
				"%s: %v", v.Src, err)
		}

		ka.attempts = v.Attempts
		ka.lastattempt = time.Unix(v.LastAttempt, 0)
		ka.lastsuccess = time.Unix(v.LastSuccess, 0)
		a.addrIndex[ka.na.ID.String()] = ka
	}

	for i := range sam.NewBuckets {
		for _, val := range sam.NewBuckets[i] {
			ka, ok := a.addrIndex[val]
			if !ok {
				return fmt.Errorf("newbucket contains %s but "+
					"none in address list", val)
			}

			if ka.refs == 0 {
				a.nNew++
			}
			ka.refs++
			a.addrNew[i][val] = ka
		}
	}

	for i := range sam.TriedBuckets {
		for _, val := range sam.TriedBuckets[i] {
			ka, ok := a.addrIndex[val]
			if !ok {
				return fmt.Errorf("tried bucket contains %s but "+
					"none in address list", val)
			}

			if ka.refs == 0 {
				a.nTried++
			}
			ka.refs++
			a.addrTried[i][val] = ka
		}
	}

	// Sanity checking.
	for k, v := range a.addrIndex {
		if v.refs == 0 && !v.tried {
			return fmt.Errorf("address %s after serialisation "+
				"with no references", k)
		}

		if v.refs > 0 && v.tried {
			return fmt.Errorf("address %s after serialisation "+
				"which is both new and tried! ", k)
		}
	}

	log.Info("Loaded %d addresses from file '%s'", a.numAddresses(), filePath)

	return nil
}

// addressHandler is the main handler for the address manager.  It must be run
// as a goroutine.
func (a *addrBook) saveRoutine() {
	path, err := filesystem.GetSpacemeshDataDirectoryPath()
	if err != nil {
		a.logger.Warning("IMPORTANT : Data directory path can't be reached. peer files are not being saved.")
		return
	}
	a.wg.Add(1)
	finalPath := path + "/" + a.localAddress.ID.String() + "/" + defaultPeersFileName

	dumpAddressTicker := time.NewTicker(saveRoutineInterval)
	defer dumpAddressTicker.Stop()

out:
	for {
		select {
		case <-dumpAddressTicker.C:
			a.savePeers(finalPath)
			a.logger.Debug("Saved peers to file %v", finalPath)

		case <-a.quit:
			break out
		}
	}

	a.logger.Debug("Saving peer before exit to file %v", finalPath)
	a.savePeers(finalPath)
	a.wg.Done()
	log.Debug("Address handler done")

}
