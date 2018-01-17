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

// Persisted node data
type NodeData struct {
	PubKey     string `json:"pubKey"`
	PrivKey    string `json:"priKey"`
	CoinBaseId string `json:"coinbase"` // coinbase account id
}

// Node store - local node data persistence functionality

// Get the os-specific full path to the nodes master data directory
// Attempts to create the directory on-demand
func ensureNodesDataDirectory() string {
	dataPath, err := filesystem.GetSpaceMeshDataDirectoryPath()
	nodesDir := filepath.Join(dataPath, nodeconfig.NodesDirectoryName)
	nodesPath, err := filesystem.GetFullDirectoryPath(nodesDir)
	if err != nil {
		log.Error("can't access spacemesh nodes folder")

	}
	return nodesPath
}

// Get the path to the node's data directory, e.g. /nodes/[node-id]/
// Directory will be created on demand if it doesn't exist
func (n *localNodeImp) ensureNodeDataDirectory() string {
	nodesDataDir := ensureNodesDataDirectory()
	nodeDirectoryName := filepath.Join(nodesDataDir, n.String())
	path, err := filesystem.GetFullDirectoryPath(nodeDirectoryName)
	if err != nil {
		log.Error("can't access node folder", n.Pretty())
	}
	return path
}

// Returns the os-specific full path to the node's data file
func getDataFilePath(nodeId string) string {
	nodesDataDir := ensureNodesDataDirectory()
	return filepath.Join(nodesDataDir, nodeId, nodeconfig.NodeDataFileName)
}

// Persist node's data to local store
func (n *localNodeImp) persistData() error {

	data := &NodeData{
		PubKey:  n.pubKey.String(),
		PrivKey: n.privKey.String(),
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Error("failed to marshal node data to json.", err)
		return err
	}

	n.ensureNodeDataDirectory()
	path := getDataFilePath(n.String())
	err = ioutil.WriteFile(path, bytes, filesystem.OwnerReadWrite)
	if err != nil {
		log.Error("failed to persist node data.", err)
	}
	return err
}

// Read node persisted data based on node id
func readNodeData(nodeId string) *NodeData {

	path := getDataFilePath(nodeId)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Error("failed to read node data from file", err)
		return nil
	}

	var nodeData NodeData
	err = json.Unmarshal(data, &nodeData)
	if err != nil {
		log.Error("failed to unmarshal nodeData", err)
		return nil
	}

	log.Info("loaded persisted node data for node id: %s", nodeId)
	return &nodeData
}

// Read node data from the data folder.
// Reads a random node from the data folder if more than one node data file is persisted
// To load a specific node on startup - users need to pass the node id using a cli arg
func readFirstNodeData() *NodeData {

	path := ensureNodesDataDirectory()
	files, err := ioutil.ReadDir(path)
	if err != nil {
		log.Error("failed to read node directory files", err)
		return nil
	}

	// only consider json files
	for _, f := range files {
		if f.IsDir() && !strings.HasPrefix(f.Name(), ".") {
			return readNodeData(f.Name())
		}
	}

	return nil
}
