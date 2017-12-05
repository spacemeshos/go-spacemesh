package node

import (
	"encoding/json"
	"github.com/UnrulyOS/go-unruly/filesystem"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/node/config"
	"io/ioutil"
	"path/filepath"
	"strings"
)

// Node store - node data persistence functionality

// Get the os-specific full path to the nodes master data directory
// Attempts to create the directory on-demand
func ensureNodeDataDirectory() string {
	dataPath, err := filesystem.GetUnrulyDataDirectoryPath()
	nodesDir := filepath.Join(dataPath, config.NodesDirectoryName)
	nodesPath, err := filesystem.GetFullDirectoryPath(nodesDir)
	if err != nil {
		log.Error("Can't access unruly nodes folder")

	}
	return nodesPath
}

// Get the path to the node's data directory, e.g. /nodes/[node-id]/
// Directory will be created on demand if it doesn't exist
func (n *Node) ensureNodeDataDirectory() string {
	nodesDataDir := ensureNodeDataDirectory()
	nodeDirectoryName := filepath.Join(nodesDataDir, n.identity.String())
	path, err := filesystem.GetFullDirectoryPath(nodeDirectoryName)
	if err != nil {
		log.Error("Can't access node %s folder", n.identity.Pretty())
	}
	return path
}

// Returns the os-specific full path to the node's data file
func getDataFilePath(nodeId string) string {
	nodesDataDir := ensureNodeDataDirectory()
	return filepath.Join(nodesDataDir, nodeId, config.NodeDataFileName)
}

// Persist node's data to local store
func (n *Node) persistData() error {

	pubKeyStr, err := n.publicKey.String()
	if err != nil {
		log.Error("Failed to get node public key str: %v", err)
		return err
	}

	privateKeyStr, err := n.privateKey.String()
	if err != nil {
		log.Error("Failed to get node private key str: %v", err)
		return err
	}

	data := &NodeData{
		Id:      n.identity.String(),
		PubKey:  pubKeyStr,
		PrivKey: privateKeyStr,
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Error("Failed to marshal node data to json: %v", err)
		return err
	}

	n.ensureNodeDataDirectory()
	path := getDataFilePath(n.identity.String())
	return ioutil.WriteFile(path, bytes, 0660)
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

	log.Info("Loading persistent node data for node id: %s", nodeId)
	return &nodeData
}

// Read node data from the data folder.
// Reads a random node from the data folder if more than one node data file is persisted
// To load a specific node on startup - pass the node id using the node cli arg
func readFirstNodeData() *NodeData {

	path := ensureNodeDataDirectory()
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
