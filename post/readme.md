## Proof of space time (POST)

### Task Overview
- Let G be table be a large table with sequential binary data entries indexed as (0,...,2^H - 1) where each entry is of equal size (say 32 bits) where there are 2^H entries for some int H.
- Leaves contain arbitrary fixed-size data (e.g. 32 bits).
- Implement and benchmark the `Merkle Traversal in log space and time` algorithm as defined [here](https://github.com/spacemeshos/go-spacemesh/blob/master/research/szydlo-loglog.pdf) -> `Algorithm 4: Logarithmic Merkle Tree Traversal`.
- The value of an internal node in the Merkle tree is the hash of its 2 children.
- Given a leaf with index i into the table, compute the Merkle Proof for the leaf.
- The Merkle proof is the set of the hashes of the siblings of the nodes on the path from the leaf to the root and the hash of the root. 
- A reference Java implementation is available here: https://github.com/wjtoth/Hash-based-signatures/blob/master/src/main/java/wjtoth/MSS/MerkleSSLogarithmic.java
- Use crypto.Sha256() for the hash function, therefore, internal node size is 32 bytes.
- `table.go` provides the table data strcuture `table_test.go` shows how to populate a table with binary data backed by a binary data file.
- This functionality is required for our proof of space time protocol.
- The main goal is to benchmark an efficient traversal implementation to be able to better model space and time parameters. In other words, how long does it take on a modern CPU to traverse a table with a large number of entries? For example, given n=36 and entry size of 1 byte - what is the space / time used on a modern Intel core to compute the Merkle proof for the last (rightmost) item in the table?

- As the size of the internal labeling can be larger than the data itself (since each internal label is 32 bytes if we use SHA-256). If generating a merkle tree is fast enough (which it hopefully will be), we might want to store just the POST data, and then regenerate the internal labeling of the Merkle tree just before we need it for the POST proof. (e.g., if it takes 10 minutes to generate, but we need to do it once every two weeks, that could be reasonable).

### Details
- Let G be a table with 2^n fixed-sized binary data entries (say 1 byte per entry).
- Implement a function that takes an index i in range [0,2^n-1] and outputs the set of the values/labels of the siblings of nodes on the path from leaf i to the entrie's Merkle root and the root node value (Merkle proof) without using lots of space (using the log Merkle Tree Traversal alog)
- We may use this function periodicaly and not store the internal nodes or to generate and store the internal nodes in the POST setup phase.

