package config

// params are non-configurable (hard-coded) consts. To create a configurable param use Config.
// add all node params here (non-configurable consts) - ideally most node params should be configurable.
const (
	ClientVersion    = "go-spacemesh/0.0.1"
	MinClientVersion = "0.0.1"
)

// NetworkID represents the network that the node lives in
// that indicates what nodes it can communicate with, and which bootstrap nodes to use
const (
	MainNet = iota
	TestNet
)
