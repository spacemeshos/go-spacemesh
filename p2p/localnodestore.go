package p2p

import (
	"encoding/json"
	"github.com/UnrulyOS/go-unruly/filesystem"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p/nodeconfig"
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
	dataPath, err := filesystem.GetUnrulyDataDirectoryPath()
	nodesDir := filepath.Join(dataPath, nodeconfig.NodesDirectoryName)
	nodesPath, err := filesystem.GetFullDirectoryPath(nodesDir)
	if err != nil {
		log.Error("Can't access unruly nodes folder")

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
		log.Error("Can't access node %s folder", n.Pretty())
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
		log.Error("Failed to marshal node data to json: %v", err)
		return err
	}

	n.ensureNodeDataDirectory()
	path := getDataFilePath(n.String())
	err = ioutil.WriteFile(path, bytes, filesystem.OwnerReadWrite)
	if err != nil {
		log.Error("Failed to persist node data. %v", err)
	}
	return err
}

// Read node persisted data based on node id
func readNodeData(nodeId string) *NodeData {

	path := getDataFilePath(nodeId)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Error("Failed to read node data from file: %v", err)
		return nil
	}

	var nodeData NodeData
	err = json.Unmarshal(data, &nodeData)
	if err != nil {
		log.Error("Failed to unmarshal nodeData. %v", err)
		return nil
	}

	log.Info("Loaded peristed node data for node id: %s", nodeId)
	return &nodeData
}

// Read node data from the data folder.
// Reads a random node from the data folder if more than one node data file is persisted
// To load a specific node on startup - pass the node id using the node cli arg
func readFirstNodeData() *NodeData {

	path := ensureNodesDataDirectory()
	files, err := ioutil.ReadDir(path)
	if err != nil {
		log.Error("Failed to read node directory files. %v", err)
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
