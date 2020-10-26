package node

import (
	"testing"
)

func TestMultiNode(t *testing.T) {
	StartMultiNode(5, 10, 10, "/tmp/data")
}
