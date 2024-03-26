package fastnet

import (
	_ "embed"
)

//go:embed "smesher.json"
var SmesherConfig string

//go:embed "poet.conf"
var PoetConfig string

//go:embed "certifier.yaml"
var CertifierConfig string
