#Terms
- Pointer = sha256(bin_encode(v)). 256 bits.
- Value = bin_encode(anything)
- Path - data domain key to value (not hash)

Persistent format
Stores tree pointers
(p/v) flat db with k = sha256(binary_encode(v))
v is binary encoded node value
First key is the root hash

Working with Nibbles
Data is stored in bytes but represented as hex chars - each char is a nibble (4 bits data)
Byte <01> is an encoding both nibble 1 and the 2 nibbles 0 ad 1
A path may have an odd number of nibbles
We need to avoid this ambiguity by encoding is a path has odd or even number of nibbles
To avoid this, the path is can be prefixed with a flag

Node Types

NULL (represented as the empty string)
Branch A 17-item node [ v0 ... v15, vt ]
16 items array - one for each nibble (hex char) - nice to use sparse arrays here and allocate 16*4 bytes per node.
Array content - child key (hash)
Leaf A 2-item node [ encodedPath, value ]
encodedPath - partial path from node to value
Extension A 2-item node [ encodedPath, pointer ]
encodedPath - partial path from node
Pointer - pointer to child node
Encoded path format
hex char    bits    |    node type partial     path length
----------------------------------------------------------
   0        0000    |       extension              even        
   1        0001    |       extension              odd         
   2        0010    |   terminating (leaf)         even        
   3        0011    |   terminating (leaf)         odd        
For even paths, add another 0 padding nibble (0000)


Working with values
We use another external table to store (k,v) where k=sha3(v) and v is arbitrary data. It is used to access the values.
If len(value) >= 32 then we can store k in the trie and add a table entry otherwise we can store v directly in the trie and avoid lookup in the external table - no need to hash it and find the value hashed by the key
To find a value we can treat it as k and do an external table lookup. If the value is not there then k is the value. Otherwise use the value from the table.







