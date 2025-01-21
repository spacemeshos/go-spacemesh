package fptree

import (
	"slices"
	"sync"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// nodeIndex represents an index of a node in the node pool.
type nodeIndex uint32

const (
	// noIndex represents an invalid node index.
	noIndex = ^nodeIndex(0)
	// leafFlag is a flag that indicates that a node is a leaf node.
	leafFlag = uint32(1 << 31)
)

// node represents an fpTree node.
type node struct {
	// Fingerprint
	fp rangesync.Fingerprint
	// Item count
	c uint32
	// Left child, noIndex if not present.
	l nodeIndex
	// Right child, noIndex if not present.
	r nodeIndex
}

// nodePool represents a pool of tree nodes.
// The pool is shared between the orignal tree and its clones.
type nodePool struct {
	mtx     sync.RWMutex
	rcPool  rcPool[node, uint32]
	leafMap map[uint32]rangesync.KeyBytes
}

// init pre-allocates the node pool with n nodes.
func (np *nodePool) init(n int) {
	np.rcPool.init(n)
}

// lockWrite locks the node pool for writing.
// There can only be one writer at a time.
// This blocks until all other reader and writer locks are released.
func (np *nodePool) lockWrite() { np.mtx.Lock() }

// unlockWrite unlocks the node pool for writing.
func (np *nodePool) unlockWrite() { np.mtx.Unlock() }

// lockRead locks the node pool for reading.
// There can be multiple reader locks held at a time.
// This blocks until the writer lock is released, if it's held.
func (np *nodePool) lockRead() { np.mtx.RLock() }

// unlockRead unlocks the node pool for reading.
func (np *nodePool) unlockRead() { np.mtx.RUnlock() }

// add adds a new node to the pool.
func (np *nodePool) add(
	fp rangesync.Fingerprint,
	c uint32,
	left, right nodeIndex,
	v rangesync.KeyBytes,
	replaceIdx nodeIndex,
) nodeIndex {
	if c == 1 || left == noIndex && right == noIndex {
		c |= leafFlag
	}
	newNode := node{fp: fp, c: c, l: noIndex, r: noIndex}
	if left != noIndex {
		newNode.l = left
	}
	if right != noIndex {
		newNode.r = right
	}
	var idx uint32
	if replaceIdx != noIndex {
		np.rcPool.replace(uint32(replaceIdx), newNode)
		idx = uint32(replaceIdx)
	} else {
		idx = np.rcPool.add(newNode)
	}
	if v != nil {
		if c != 1|leafFlag {
			panic("BUG: non-leaf node with a value")
		}
		if np.leafMap == nil {
			np.leafMap = make(map[uint32]rangesync.KeyBytes)
		}
		np.leafMap[idx] = slices.Clone(v)
	} else if replaceIdx != noIndex {
		delete(np.leafMap, idx)
	}
	return nodeIndex(idx)
}

// value returns the value of the node at the given index.
func (np *nodePool) value(idx nodeIndex) rangesync.KeyBytes {
	if idx == noIndex {
		return nil
	}
	return np.leafMap[uint32(idx)]
}

// left returns the left child of the node at the given index.
func (np *nodePool) left(idx nodeIndex) nodeIndex {
	if idx == noIndex {
		return noIndex
	}
	node := np.rcPool.item(uint32(idx))
	if node.c&leafFlag != 0 || node.l == noIndex {
		return noIndex
	}
	return node.l
}

// right returns the right child of the node at the given index.
func (np *nodePool) right(idx nodeIndex) nodeIndex {
	if idx == noIndex {
		return noIndex
	}
	node := np.rcPool.item(uint32(idx))
	if node.c&leafFlag != 0 || node.r == noIndex {
		return noIndex
	}
	return node.r
}

// leaf returns true if this is a leaf node.
func (np *nodePool) leaf(idx nodeIndex) bool {
	if idx == noIndex {
		panic("BUG: bad node index")
	}
	node := np.rcPool.item(uint32(idx))
	return node.c&leafFlag != 0
}

// count returns number of set items to which the node at the given index corresponds.
func (np *nodePool) count(idx nodeIndex) uint32 {
	if idx == noIndex {
		return 0
	}
	node := np.rcPool.item(uint32(idx))
	if node.c == 1 {
		panic("BUG: single-count node w/o the leaf flag")
	}
	return node.c &^ leafFlag
}

// info returns the count, fingerprint, and leaf flag of the node at the given index.
func (np *nodePool) info(idx nodeIndex) (count uint32, fp rangesync.Fingerprint, leaf bool) {
	if idx == noIndex {
		panic("BUG: bad node index")
	}
	node := np.rcPool.item(uint32(idx))
	if node.c == 1 {
		panic("BUG: single-count node w/o the leaf flag")
	}
	return node.c &^ leafFlag, node.fp, node.c&leafFlag != 0
}

// releaseOne releases the node at the given index, returning it to the pool.
func (np *nodePool) releaseOne(idx nodeIndex) bool {
	if idx == noIndex {
		return false
	}
	if np.rcPool.release(uint32(idx)) {
		delete(np.leafMap, uint32(idx))
		return true
	}
	return false
}

// release releases the node at the given index, returning it to the pool, and recursively
// releases its children.
func (np *nodePool) release(idx nodeIndex) bool {
	if idx == noIndex {
		return false
	}
	node := np.rcPool.item(uint32(idx))
	if !np.rcPool.release(uint32(idx)) {
		return false
	}
	if node.c&leafFlag == 0 {
		if node.l != noIndex {
			np.release(node.l)
		}
		if node.r != noIndex {
			np.release(node.r)
		}
	} else {
		delete(np.leafMap, uint32(idx))
	}
	return true
}

// ref adds a reference to the given node.
func (np *nodePool) ref(idx nodeIndex) {
	np.rcPool.ref(uint32(idx))
}

// refCount returns the reference count for the node at the given index.
func (np *nodePool) refCount(idx nodeIndex) uint32 {
	return np.rcPool.refCount(uint32(idx))
}

// nodeCount returns the number of nodes in the pool.
func (np *nodePool) nodeCount() int {
	return np.rcPool.count()
}
