package node

import (
	"github.com/UnrulyOS/go-unruly/assert"
	"github.com/UnrulyOS/go-unruly/filesystem"
	"testing"
)

func TestNodeCreation(t *testing.T) {

	// start fresh
	filesystem.DeleteUnrulyDataFolders(t)

	done := make(chan bool, 1)
	node := NewNode(6666, done)

	assert.NotNil(t, node, "expected non-nil node")

	// todo: verify that node info was persisted to /nodes/node-id/id.json

	// test all of these cases:

	// start node where id.json exists

	// start node using new id

}
