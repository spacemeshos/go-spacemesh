package fastnet

import (
	_ "embed"
)

//go:embed "smesher.json"
var SmesherConfig string

//go:embed "poet.json"
var PoetConfig string
