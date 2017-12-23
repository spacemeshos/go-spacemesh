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
	BootstrapNodes = []string{
		"126.0.0.1:3572/QmcjTLy94HGFo4JoYibudGeBV2DSBb6E4apBjFsBGnMsWa",
		"125.0.0.1:3763/QmRtrUMB3rfmRZE6yn8yLRvik6a5Pprvc5HnB1HT8MnoPy",
	}
)
