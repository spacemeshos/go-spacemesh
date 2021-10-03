package node

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
)

// nodeFileData defines persistent node data.
type nodeFileData struct {
	PubKey  string `json:"pubKey"`
	PrivKey string `json:"priKey"`
}

// Node store - local node data persistence functionality

// PersistData save node's data to local disk in `path`.
func (n *LocalNode) PersistData(path string) error {
	data := nodeFileData{
		PubKey:  n.publicKey.String(),
		PrivKey: n.privKey.String(),
	}

	finaldata, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	datadir := filepath.Join(path, config.P2PDirectoryPath, config.NodesDirectoryName, n.publicKey.String())
	if err = filesystem.ExistOrCreate(datadir); err != nil {
		return fmt.Errorf("failed to check or create datadir: %w", err)
	}

	nodefile := filepath.Join(datadir, config.NodeDataFileName)

	// make sure our node file is written to the os filesystem.
	if err := ioutil.WriteFile(nodefile, finaldata, 0666); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// TODO(josebalius): should we accept a logger or otherwise not log directly
	log.Info("Saved p2p node information (%s),  Identity - %v", nodefile, n.publicKey.String())

	return nil
}

// LoadIdentity loads a specific nodeID from the disk at the given path.
func LoadIdentity(path, nodeID string) (ln LocalNode, err error) {
	nfd, err := readNodeData(path, nodeID)
	if err != nil {
		return ln, fmt.Errorf("failed to read node data: %w", err)
	}

	return newLocalNodeFromFile(nfd)
}

// Read node persisted data based on node id.
func readNodeData(path string, nodeID string) (*nodeFileData, error) {
	nodefile := filepath.Join(path, config.P2PDirectoryPath, config.NodesDirectoryName, nodeID, config.NodeDataFileName)
	if !filesystem.PathExists(nodefile) {
		return nil, fmt.Errorf("tried to read node from non-existing path %q", nodefile)
	}

	b, err := ioutil.ReadFile(nodefile)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var nodeData nodeFileData
	err = json.Unmarshal(b, &nodeData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	return &nodeData, nil
}

func getLocalNodes(path string) ([]string, error) {
	nodesDir := filepath.Join(path, config.P2PDirectoryPath, config.NodesDirectoryName)
	if !filesystem.PathExists(nodesDir) {
		return nil, fmt.Errorf("directory not found %q", path)
	}

	fls, err := ioutil.ReadDir(nodesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read dir: %q: %w", nodesDir, err)
	}

	keys := make([]string, len(fls))
	for i, f := range fls {
		keys[i] = f.Name()
	}

	return keys, nil
}

// ReadFirstNodeData reads node data from the data folder.
// It reads the first node from the data folder.
func ReadFirstNodeData(path string) (ln LocalNode, err error) {
	nds, err := getLocalNodes(path)
	if err != nil {
		return ln, fmt.Errorf("failed to get local nodes: %w", err)
	}

	f, err := readNodeData(path, nds[0])
	if err != nil {
		return ln, fmt.Errorf("failed to read node data: %w", err)
	}

	return newLocalNodeFromFile(f)
}
