package node

import (
	"testing"
)

func TestMultiNode(t *testing.T) {
	StartMultiNode(10, 10, 10, "/tmp/data")
}
