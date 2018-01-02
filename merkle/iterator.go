package merkle

/*
type treeIterator interface {


}


type treeIteratorImp struct {
	abort bool
	onDone chan []NodeContainer
	onNode chan NodeContainer
	root NodeContainer
}

func newTreeIterator(r NodeContainer, done chan []NodeContainer, node chan NodeContainer) treeIterator {
	iter := &treeIteratorImp {
		onDone: done,
		onNode: node,
		root: r,
	}

	return iter
}


func (mt *merkleTreeImp) walk(root NodeContainer, onNode func([]NodeContainer), onDone func(error) ) {

	if root == nil {
		root = mt.root
	}

	if root == nil  {
		onDone(nil)
	}

	results := []NodeContainer{}
	abort := false

	err := root.loadChildren(mt.treeData)
	if err != nil {
		onDone(err)
	}

}

func processNode(n NodeContainer, key string, callback func(error)) {
	if n == nil {
		callback(nil)
	}
}*/
