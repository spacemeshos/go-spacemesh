# Design Notes
A Merkle tree supporting CRUD ops for user (k,v) data backed by a (k,v) data store.
Note that this is actually more accurately named trie which is different form the classic definition of a markle tree - a complete binary tree with values at leaves where each pointer from parent to child is a hash of the child's value  and a non-leaf value is a hash of the union of is pointers to children.

## Terms
- `User data domain`: client stored data (k,v).
- `Tree data domain`: data use internally in the tree without client awareness.
- `v`: user data domain value. a bin_encode(of any data)
- `k`: pointer to value in the user data domain. User provided. Can be any binary encoded data. Doesn't need to be hash(v). 
- `Path`: the user data domain key to a value (not hash or tree pointer to node). Hex encoded binary data.
- `Partial path`: a partial segment of the path. Hex encoded binary data. Note that bath can be nibbles.
- `P`: a pointer from a tree node to another node - in the data tree domain. The tree must be acyclic so each pointer is from a node to a lower level node.
A Pointer value is always a hash of the linked node binary encoding. e.g. p==sha3(linked-node-binary-value). The value of a node is its content (see below).
. Pointers are only used internally to maintain the tree structure and are not user inserted keys. Keys are use set. e.g. Add(k,v)

The goals of this data structure is to have o(log(n)) lookup, insert, update and delete ops time on expense of additional o(log(n)) storage and to enable efficient Merkle proofs - verification if given binary data is stored or not.

## Persistence
- 2 flat tables are used: 
    - An external table with (k,v) pairs - this is the data domain. 
    - Another internal to hold the tree structure (p,v) - the tree can be reconstructed from this table.
- The external table is used to get values based on their keys.
- The internal table completely represents a Merkle tree and is used to load it to memory and persist it.
- The internal table keys are pointers and the values are a binary representation of a table node.
- The values of branch nodes in the internal table are keys (k) to data-domain values if len(bin_encode(v))<256 bits or bin_encode(v) otherwise. 
To find a value we can treat it as k and do an external table lookup. If the value is not there then k is the value. Otherwise use the value from the table.
- First key in the internal table is the tree the root (hash)
- Persistence should be implemented using [go-leveldb](https://github.com/syndtr/goleveldb)

## Working with nibbles
- A path in the tree may have an odd or an even number of nibbles.
- We avoid having to deal with parity issues by using hex encoded strings to represent paths. 
- Hex encoded strings can represent paths with odd number of nibbles as each hex char represents one nibble (4 bits of data with 16 possible values)
- We avoid working with []byte for paths as it can't represent a path with an odd number of nibbles without padding and special handling.

## Node Types
- Branch node
    - A 17-items node [ v0 ... v15, value ]
    - First 16 items: A Nibbles array with one entry for each nibble (hex char). Each array index is either nil or a pointer P to a child node.
    - Each possible hex char in a path hex representation is represented in the array.
    - value: a value the terminates in the path to this node or nil.
    - value is nil or k of v if bin-encode(v) > 256 bits or the bin value itself v otherwise.
- Extension node
    - Optimization of a branch consisting of many branch nodes, each with only 1 pointer to a child.
    - A 2-item node [ encodedPath, pointer ]
    - encodedPath - partial path from parent's path
    - pointer - pointer to a child node - always an hex-encoded 256 bits hash
- Leaf node
    - A 2-item node [ encodedPath, value ]
    - encodedPath - partial path from parent to value. Optimization.
    - Value is k of v if bin-encode(v) > 256 bits or the bin value itself v otherwise.
- Empty node
    - Hash is a sha3(hexStringEncode(empty-string))
      
- Short node: an Extension or a Leaf node.
- Full node: a branch node.
   
## Serialization and implementation nodes
- We use a `shortNode` type to represent both leaf and extension node and `branchNode` for branch nodes.
- We use a `nodeContainer` type to provide a type-safe access to nodes.
- We do not need to add a termination flag to value of a short node (as done in eth) as we have a notion of distinct extension and leaf nodes types.


### Additional Info
Some of the optimization ideas we use for Merkle trees are from `ethereum tries`. 
As described above, we use our own different optimizations in some cases. For example, we do not use path prefixed metadata.

More info here:
- js implementation of the eth Merkle tries: https://github.com/ethereumjs/merkle-patricia-tree
- https://easythereentropy.wordpress.com/2014/06/04/understanding-the-ethereum-trie/
- https://blog.ethereum.org/2015/11/15/merkling-in-ethereum/
- https://github.com/ethereum/wiki/wiki/Patricia-Tree


### Find node algorithm

func findNode(r,k,pos,s) (v,err)
- input r: tree root node
- input k: path to value/ (hex encoded nibbles)
- input pos: number of nibbles matched in path to value
- in/out s: path to node with value if found or of nodes to where node with value should have been if not found
- output value: nil if not found, []byte otherwise

Implementation:
- if r is nil return nil
- load r children from store to memory
- push r to s
- if r is branch then...
    - if the whole path k matched then return value stored in r
    - if r has a child for last nibble of k then return findNode(child,k,pos+1,s) else return nil
- else if r is ext node then...
    - let p be partial path encoded in r
    - if len(k)-pos < len(p) || path != k[pos:pos+len(path)] return nil
    - let c be the child node pointed by p
    - return findValue(c,k,pos+len(p),s)
- else if r is a leaf node then...
    - let p be the partial path encoded in r
    - if len(k)-pos < len(p) || path != k[pos:pos+len(path)] return nil
    - found - return value(p)    
    
### Delete node algorithm

Algo 1 - func delete(k)
- input k: path to value - hex encoded value
Implementation:
- let s be a new nodes tack and r the tree's root node
- call findNode(r,k, 0, s)
    - if result not found return err
- s now has the path to the node which is holding that value
- the node in s.top is the node to delete or to update
- call deleteNode(k,s)

func deleteNode(k,s)
- k: path to node to delete
- s: nodes path from root to node to be deleted/updated
- s.root - node to delete or update
Implementation:
- let lastnode = s.pop()
- if lastnode is nil return nil
- let parentnode = s.pop()
- if parentnode is nil then lastnode is a root leaf node - remove it from tree and return nil
- if lastnode is a branch node then it is holding the value to be removed from the tree
    - set value to nil and return
- else lastnode is a leaf node and its parent is a branch...
    - let p be the path embedded in lastnode
    - k = k[:len(k)-len(path)]
    - remove lastnode from the tree
    - idx = k(len(k)01) - branch node idx of lastnode
    - remove child at idx from branch node
    - k = k[:len(k)-2] - remove last nibble from k
    - lastnode <- parentnode
    - parentnode = s.pop ()
 - lastnode is a branch...
    - if it has only 1 child then collapse it to ext node
        - todo: document processBranchNode() here
    - else s.push(parentNode)
 - update all node pointers on the path specified by s 
 - return
  
  
 func processBranchNode()
 
 Impl:
 - todo: complete me.
 
 -------
 
 Algo 2 - func delete(r, k, pos) (bool, root)
 r: tree root
 k: path to value to delete
 pos: nibbles matched on path
 
 root: returns new tree root
 bool: true if deleted
 
 execution: call delete(root, k, 0) to delete value keyed by k
 
 implementation:
 - if r is ext or leaf then...
    - len := commonPrefix(k[pos:],r.path)
    - if len < len(r.path) return (false, r)
    - if len == len(r.path) remove r from trie, return (true,nil)