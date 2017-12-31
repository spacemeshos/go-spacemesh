# Design Notes

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

## main Node Types
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







