package node

import (
	"testing"
)

func TestMultiNode(t *testing.T) {
	StartMultiNode(5, 10, 100, "/tmp/data")
}
