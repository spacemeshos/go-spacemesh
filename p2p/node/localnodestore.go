package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

// nodeFileData defines persistent node data.
type nodeFileData struct {
	PubKey  string `json:"pubKey"`
	PrivKey string `json:"priKey"`
}

// Node store - local node data persistence functionality

// Persist node's data to local store.
func (n *LocalNode) PersistData(path string) error {

	data := nodeFileData{
		PubKey:  n.publicKey.String(),
		PrivKey: n.privKey.String(),
	}

	finaldata, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	datadir := filepath.Join(path, config.P2PDirectoryPath, config.NodesDirectoryName, n.publicKey.String())

	err = filesystem.ExistOrCreate(datadir)
	if err != nil {
		return err
	}

	nodefile := filepath.Join(datadir, config.NodeDataFileName)

	// make sure our node file is written to the os filesystem.
	f, err := os.Create(nodefile)
	if err != nil {
		return err
	}

	_, err = f.Write(finaldata)
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

	log.Info("Saved p2p node information (%s),  Identity - %v", nodefile, n.publicKey.String())

	return nil
}

func LoadIdentity(path, nodeid string) (LocalNode, error) {
	nfd, err := readNodeData(path, nodeid)
	if err != nil {
		return emptyNode, err
	}

	return newLocalNodeFromFile(nfd)
}

// Read node persisted data based on node id.
func readNodeData(path string, nodeID string) (*nodeFileData, error) {

	nodefile := filepath.Join(path, config.P2PDirectoryPath, config.NodesDirectoryName, nodeID, config.NodeDataFileName)

	if !filesystem.PathExists(nodefile) {
		return nil, fmt.Errorf("tried to read node from non-existing path (%v)", nodefile)
	}

	data := bytes.NewBuffer(nil)

	f, err := os.Open(nodefile)
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

	return &nodeData, nil
}

func getLocalNodes(path string) ([]string, error) {
	nodesDir := filepath.Join(path, config.P2PDirectoryPath, config.NodesDirectoryName)

	if !filesystem.PathExists(nodesDir) {
		return nil, fmt.Errorf("directory not found %v", path)
	}

	fls, err := ioutil.ReadDir(nodesDir)

	if err != nil {
		return nil, err
	}

	keys := make([]string, len(fls))

	for i, f := range fls {
		keys[i] = f.Name()
	}

	return keys, nil
}

// Read node data from the data folder.
// Reads a random node from the data folder if more than one node data file is persisted.
// To load a specific node on startup - users need to pass the node id using a cli arg.
func ReadFirstNodeData(path string) (LocalNode, error) {
	nds, err := getLocalNodes(path)
	if err != nil {
		return emptyNode, err
	}

	f, err := readNodeData(path, nds[0])
	if err != nil {
		return emptyNode, err
	}

	return newLocalNodeFromFile(f)
}
