## Terms

- data domain: client stored data.
- tree domain data: data use internally in the tree without client awareness.
- v: data domain value. a bin_encode(of any data)
- k: pointer to value in the data domain. k==sha3(v)
- Path: the data's domain key to a value (not hash or tree pointer to node)
- Partial path: a partial segment of the full path
- P: a pointer from a node to another node - in the tree domain. The trie must be acyclic so each pointer is from a node to a lower level node.
- A Pointer value is always a has of the linked node binary encoding. e.g. p==sha3(linked-node-binary-value)
. Pointers are only used internally to maintain the data in the tree and are not user inserted keys. Keys are use set. e.g. Add(k,v)

## Persistence
- 2 flat tables are used. 
    - An external table with (k,v) pairs - this is the data domain. 
    - Another internal to hold the tree structure (p,v) - the tree can be reconstructed from this table.
- The external table is used to get values based on their keys.
- The internal table completely represents a Merkle tree and is used to load it to memmory and persist it.
- The internal table keys are pointers and the values are a binary representation of a table node.
- The values of branch nodes in the internal table are keys (k) to data-domain values if len(bin_encode(v))<256 bits or bin_encode(v) otherwise. 
To find a value we can treat it as k and do an external table lookup. If the value is not there then k is the value. Otherwise use the value from the table.


## Persistence format
- Stores tree pointers
- (p/v) flat db with  being the key of v
- v is binary encoded node value
- First key is the root hash

## Working with nibbles
- Data is stored in bytes but represented as hex chars - each hex char represents a nibble (4 bits data)
- Byte <01> is an encoding both one nibble with the value 0001 and the 2 nibbles with values 0000 and 0001
- A path may have an odd number of nibbles
- We need to avoid this ambiguity by encoding is a path has odd or even number of nibbles
- To avoid this, the path is prefixed with a flag

## Node Types
- NULL node
    - represented as the empty string
- Branch node
    - A 17-item node [ v0 ... v15, vt ]
    - 16 items array - one for each nibble (hex char) - nice to use sparse arrays here and allocate 16*4 bytes per node.
    - Array content - pointer to child node (hash)
- Leaf node
    - A 2-item node [ encodedPath, value ]
    - encodedPath - partial path from node to value
    - value is K of V if bin-encode(v) > 256 bits or the bin value itself V otherwise.
- Extension node
    - A 2-item node [ encodedPath, pointer ]
    - encodedPath - partial path from node
    - pointer - pointer to a child node

## Encoded path format

| hex char |  bits   |    node type partial    |  path length |
|------|-------------|-------------------------|--------------|
|  0   |     0000    |       extension         |     even     |   
|  1   |     0001    |       extension         |     odd      |    
|  2   |     0010    |   terminating (leaf)    |    even      |  
|  3   |     0011    |   terminating (leaf)    |     odd      |
  
For even paths, add another 0 padding nibble (0000)









