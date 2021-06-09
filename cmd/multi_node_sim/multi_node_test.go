package main

import (
	"github.com/spacemeshos/go-spacemesh/cmd/node"
	"testing"
)

func TestMultiNode(t *testing.T) {
	node.StartMultiNode(5, 10, 10, "/tmp/data")
}
