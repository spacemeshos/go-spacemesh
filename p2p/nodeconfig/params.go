package nodeconfig

// params are non-configurable (hard-coded) consts. To create a configurable param use Config.
// add all node params here (non-configurable consts) - ideally most node params should be configurable.
const (
	ClientVersion      = "go-spacemesh-node/0.0.1"
	NodesDirectoryName = "nodes"
	NodeDataFileName   = "id.json"
	NodeDbFileName     = "node.db"
)
