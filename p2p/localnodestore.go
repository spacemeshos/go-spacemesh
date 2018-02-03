package p2p

import (
	"encoding/json"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"io/ioutil"
	"path/filepath"
	"strings"
)

// NodeData defines persistent node data.
type NodeData struct {
	PubKey     string `json:"pubKey"`
	PrivKey    string `json:"priKey"`
	CoinBaseID string `json:"coinbase"` // coinbase account id
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
// Directory will be created on demand if it doesn't exist
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
		PubKey:  n.pubKey.String(),
		PrivKey: n.privKey.String(),
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

	return ioutil.WriteFile(path, bytes, filesystem.OwnerReadWrite)
}

// Read node persisted data based on node id.
func readNodeData(nodeID string) (*NodeData, error) {

	path, err := getDataFilePath(nodeID)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var nodeData NodeData
	err = json.Unmarshal(data, &nodeData)
	if err != nil {
		return nil, err
	}

	log.Info("loaded persisted node data for node id: %s", nodeID)
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
		if f.IsDir() && !strings.HasPrefix(f.Name(), ".") {
			return readNodeData(f.Name())
		}
	}

	return nil, nil
}
