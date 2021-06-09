package main

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/cmd/node"
)

func TestMultiNode(t *testing.T) {
	node.StartMultiNode(5, 10, 10, "/tmp/data")
}
