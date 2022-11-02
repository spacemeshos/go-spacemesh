package fastnet

import (
	_ "embed"
)

//go:embed "smesher.json"
var SmesherConfig []byte

//go:embed "poet.json"
var PoetConfig []byte
