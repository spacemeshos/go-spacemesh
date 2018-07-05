package node

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"strings"

	"bytes"
	"io"

	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
)

// nodeFileData defines persistent node data.
type nodeFileData struct {
	PubKey     string `json:"pubKey"`
	PrivKey    string `json:"priKey"`
	CoinBaseID string `json:"coinbase"` // coinbase account id
	NetworkID  int    `json:"network"`  // network that the node lives in
}

// Node store - local node data persistence functionality

// Persist node's data to local store.
func (n *LocalNode) persistData() error {

	data := nodeFileData{
		PubKey:    n.pubKey.String(),
		PrivKey:   n.privKey.String(),
		NetworkID: int(n.networkID),
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	nodeDataPath, err := filesystem.EnsureNodesDataDirectory(nodeconfig.NodesDirectoryName)
	if err != nil {
		return err
	}

	path := filesystem.NodeDataFile(nodeDataPath, nodeconfig.NodeDataFileName, n.String())

	// make sure our node file is written to the os filesystem.
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	_, err = f.Write(bytes)
	if err != nil {
		return err
	}

	err = f.Sync()
	if err != nil {
		return err
	}

	err = f.Close()
	if err != nil {
		return err
	}

	log.Info("Saved node information. NodeID %v", n.String())

	return nil
}

// Read node persisted data based on node id.
func readNodeData(nodeID string) (*nodeFileData, error) {

	nodeDataPath, err := filesystem.EnsureNodesDataDirectory(nodeconfig.NodesDirectoryName)
	if err != nil {
		return nil, err
	}

	path := filesystem.NodeDataFile(nodeDataPath, nodeconfig.NodeDataFileName, nodeID)

	data := bytes.NewBuffer(nil)

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(data, f)

	if err != nil {
		return nil, err
	}

	err = f.Close()

	if err != nil {
		return nil, err
	}

	var nodeData nodeFileData
	err = json.Unmarshal(data.Bytes(), &nodeData)
	if err != nil {
		return nil, err
	}

	log.Debug("loaded persisted node data for node id: %s", nodeID)
	return &nodeData, nil
}

// Read node data from the data folder.
// Reads a random node from the data folder if more than one node data file is persisted.
// To load a specific node on startup - users need to pass the node id using a cli arg.
func readFirstNodeData() (*nodeFileData, error) {

	nodeDataPath, err := filesystem.EnsureNodesDataDirectory(nodeconfig.NodesDirectoryName)
	if err != nil {
		return nil, err
	}

	files, err := ioutil.ReadDir(nodeDataPath)
	if err != nil {
		return nil, err
	}

	// only consider json files
	for _, f := range files {
		n := f.Name()
		// make sure we get only a real node file
		if f.IsDir() && !strings.HasPrefix(n, ".") {
			p := filesystem.NodeDataFile(nodeDataPath, nodeconfig.NodeDataFileName, n)
			if filesystem.PathExists(p) {
				return readNodeData(n)
			}
		}
	}

	return nil, nil
}
