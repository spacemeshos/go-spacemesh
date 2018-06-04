package nodeconfig

// params are non-configurable (hard-coded) consts.
// To create a configurable param use Config.
// Add all node params here (non-configurable consts) - ideally
// most node params should be configurable.
const (
	ClientVersion      = "go-spacemesh-node/0.0.1"
	MinClientVersion   = "0.0.1"
	NodesDirectoryName = "nodes"
	NodeDataFileName   = "id.json"
	NodeDbFileName     = "node.db"
)

// NetworkID represents the network that the node lives in
// that indicates what nodes it can communicate with,
// and which bootstrap nodes to use.
type NetworkID int32

// NetworkID types.
const (
	MainNet NetworkID = iota
	TestNet
)
