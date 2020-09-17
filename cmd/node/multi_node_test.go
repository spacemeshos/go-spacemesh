package node

import (
	"testing"
)

func TestMultiNode(t *testing.T) {
	t.Skip()
	StartMultiNode(5, 10, 10, "/tmp/data")
}
