package nodeconfig

import ()

// params are non-configurable (hard-coded) consts. To create a configurable param use Config

const (

	// add all node params here (non-configurable consts) - ideally most node params should be configurable

	ClientVersion      = "go-p2p-node/0.0.1"
	NodesDirectoryName = "nodes"
	NodeDataFileName   = "id.json"
)

var (
	BootstrapNodes = []string{ // these are the unruly foundation bootstrap nodes
		"125.0.0.1:3572/iaMujEYTByKcjMZWMqg79eJBGMDm8ADsWZFdouhpfeKj",
		"125.0.0.1:3763/x34UDdiCBAsXmLyMMpPQzs313B9UDeHNqFpYsLGfaFvm",
	}
)
