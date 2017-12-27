# Design Notes

## Terms

- `User data domain`: client stored data (k,v).
- `Tree data domain`: data use internally in the tree without client awareness.
- `v`: user data domain value. a bin_encode(of any data)
- `k`: pointer to value in the user data domain. k==sha3(v)
- `Path`: the user data domain key to a value (not hash or tree pointer to node)
- `Partial path`: a partial segment of the path.
- `P`: a pointer from a tree node to another node - in the data tree domain. The tree must be acyclic so each pointer is from a node to a lower level node.
A Pointer value is always a hash of the linked node binary encoding. e.g. p==sha3(linked-node-binary-value). The value of a node is its content (see below).
. Pointers are only used internally to maintain the tree structure and are not user inserted keys. Keys are use set. e.g. Add(k,v)

The goals of this data structure is to have o(log(n)) lookup, insert, update and delete ops time on expense of additional o(log(n)) storage and to enable efficient Merkle proofs - verification if given binary data is stored or not.

## Persistence
- 2 flat tables are used: 
    - An external table with (k,v) pairs - this is the data domain. 
    - Another internal to hold the tree structure (p,v) - the tree can be reconstructed from this table.
- The external table is used to get values based on their keys.
- The internal table completely represents a Merkle tree and is used to load it to memmory and persist it.
- The internal table keys are pointers and the values are a binary representation of a table node.
- The values of branch nodes in the internal table are keys (k) to data-domain values if len(bin_encode(v))<256 bits or bin_encode(v) otherwise. 
To find a value we can treat it as k and do an external table lookup. If the value is not there then k is the value. Otherwise use the value from the table.
- First key in the internal table is the tree the root (hash)
- Persistence should be implemented using go [leveldb](https://github.com/syndtr/goleveldb)

## Working with nibbles
- Data is stored in bytes but represented as hex chars - each hex char represents a nibble (4 bits of data with 16 possible values)
- There's some ambiguity when decoding bytes to nibbles - Byte <01> is an encoding of both one nibble with the value of 0001 and the 2 nibbles with values 0000 and 0001.
- A path may have an odd or an even number of nibbles
- We avoid this ambiguity by encoding path's parity (odd or even) in the path binary format by prefixing a path with metadata.

## main Node Types
- Branch node
    - A 17-items node [ v0 ... v15, value ]
    - First 16 items: A Nibbles array with one entry for each nibble (hex char). Each array index is either nil or a pointer P to a child node.
    - Each possible hex char in a path hex representation is represented in the array.
    - value (last item): a value the terminates in the path to this node or nil.
    - value is nil or k of v if bin-encode(v) > 256 bits or the bin value itself v otherwise.
- Extension node
    - Optimization of a branch consisting of many branch nodes, each with only 1 pointer to a child.
    - A 2-item node [ encodedPath, pointer ]
    - encodedPath - partial path from parent
    - pointer - pointer to a child node
- Leaf node
    - A 2-item node [ encodedPath, value ]
    - encodedPath - partial path from parent to value. Optimization.
    - Value is k of v if bin-encode(v) > 256 bits or the bin value itself v otherwise.
- Empty node
    - Encoded as hexStringEncode(empty-string)
      
- Short node: An Extension or Leaf node
- Full node: A branch node.
   
## Encoded path format
- Encoded paths have a 1 or 2 nibbles prefix that describe which type of node they are part of.
- Based on the prefix, the node's type is determined and we can know how to treat the data in the pointer/value part of the node.
e.g. a pointer to another node in an extension node or a 'value' in leaf nodes (which may be a key to a large value or a small value).

### First Nibble

| hex char |  bits   |    node type partial    |  path length |
|------|-------------|-------------------------|--------------|
|  0   |     0000    |       Extension         |    even      |   
|  1   |     0001    |       Extension         |     odd      |    
|  2   |     0010    |          Leaf           |    even      |  
|  3   |     0011    |          Leaf           |     odd      |
  
### Second Nibble
- For even paths, add another 0 padding nibble <0000> is added after the first nibble:
- Odd path: <meta-data-nible><path-data-nibbles>
- Event path: <meta-data-nible><0000><path-data-nibbles>

### Additional Explainers

The optimization ideas for Merkle trees are from `ethereum tries`. More info here:

- https://easythereentropy.wordpress.com/2014/06/04/understanding-the-ethereum-trie/
- https://blog.ethereum.org/2015/11/15/merkling-in-ethereum/
- https://github.com/ethereum/wiki/wiki/Patricia-Tree










