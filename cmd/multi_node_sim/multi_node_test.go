package main

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/cmd/node"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestMultiNode(t *testing.T) {
	t.Skip("temporarily disabled")

	node.StartMultiNode(logtest.New(t), 5, 10, 10, "/tmp/data")
}
