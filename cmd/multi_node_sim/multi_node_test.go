package main

import (
	"github.com/spacemeshos/go-spacemesh/cmd/node"
	"testing"
)

func TestMultiNode(t *testing.T) {
	t.Skip("temporarily disabled")

	node.StartMultiNode(5, 10, 10, "/tmp/data")
}
