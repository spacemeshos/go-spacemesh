package p2p

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"bytes"
	"io"

	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
)

// NodeData defines persistent node data.
type NodeData struct {
	PubKey     string `json:"pubKey"`
	PrivKey    string `json:"priKey"`
	CoinBaseID string `json:"coinbase"` // coinbase account id
	NetworkID  int    `json:"network"`  // network that the node lives in
}

// Node store - local node data persistence functionality.

// Gets the os-specific full path to the nodes master data directory.
// Attempts to create the directory on-demand.
func ensureNodesDataDirectory() (string, error) {
	dataPath, err := filesystem.GetSpacemeshDataDirectoryPath()
	if err != nil {
		return "", err
	}

	nodesDir := filepath.Join(dataPath, nodeconfig.NodesDirectoryName)
	return filesystem.GetFullDirectoryPath(nodesDir)
}

// Gets the path to the node's data directory, e.g. /nodes/[node-id]/
// Directory will be created on demand if it doesn't exist.
func (n *localNodeImp) EnsureNodeDataDirectory() (string, error) {
	nodesDataDir, err := ensureNodesDataDirectory()
	if err != nil {
		return "", err
	}
	nodeDirectoryName := filepath.Join(nodesDataDir, n.String())
	return filesystem.GetFullDirectoryPath(nodeDirectoryName)
}

// Returns the os-specific full path to the node's data file.
func getDataFilePath(nodeID string) (string, error) {
	nodesDataDir, err := ensureNodesDataDirectory()
	if err != nil {
		return "", err
	}

	return filepath.Join(nodesDataDir, nodeID, nodeconfig.NodeDataFileName), nil
}

// Persist node's data to local store.
func (n *localNodeImp) persistData() error {

	data := &NodeData{
		PubKey:    n.pubKey.String(),
		PrivKey:   n.privKey.String(),
		NetworkID: n.config.NetworkID,
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	_, err = n.EnsureNodeDataDirectory()
	if err != nil {
		return err
	}

	path, err := getDataFilePath(n.String())
	if err != nil {
		return err
	}

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

	log.Debug("Node data persisted and synced. NodeID ", n.String())

	return nil
}

// Read node persisted data based on node id.
func readNodeData(nodeID string) (*NodeData, error) {

	path, err := getDataFilePath(nodeID)
	if err != nil {
		return nil, err
	}

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

	var nodeData NodeData
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
func readFirstNodeData() (*NodeData, error) {

	path, err := ensureNodesDataDirectory()
	if err != nil {
		return nil, err
	}

	files, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	// only consider json files
	for _, f := range files {
		n := f.Name()
		// make sure we get only a real node file
		if f.IsDir() && !strings.HasPrefix(n, ".") {
			p, err := getDataFilePath(n)
			if err != nil {
				return nil, err
			}
			if filesystem.PathExists(p) {
				return readNodeData(n)
			}
		}
	}

	return nil, nil
}
