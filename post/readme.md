## Proof of space time (POST)

### Task Overview
- Let G be table be a large table with sequential binary data entries indexed as (0,...,2^H - 1) where each entry is of equal size (say 32 bits) where there are 2^H entries for some int H.
- Implement and benchmark the `Merkle Traversal in log space and time` as defined [here](https://github.com/spacemeshos/go-spacemesh/blob/master/research/szydlo-loglog.pdf) -> Algorithm 4 `Algorithm 4: Logarithmic Merkle Tree Traversal`.
- Given a leaf with data with index i into the table, compute the Merkle Proof for the leaf.
- The value of an internal node is the hash of its 2 children.
- The Merkle proof is the set of the hashes of the siblings of the nodes on the path from the leaf to the root and the hash of the root. 
- Reference Java implementation is available here: https://github.com/wjtoth/Hash-based-signatures/blob/master/src/main/java/wjtoth/MSS/MerkleSSLogarithmic.java
- Use crypto.Sha256() for the has function.
- `table.go` provides the table data strcuture `table_test.go` shows how to populate a table with binary data backed by a binary data file.
- This functionality is required for our proof of space time protocol.
- The main goal is to benchmark an efficient traversal implementation to be able to better model space and time parameters. In other words, how long does it take on a modern CPU to traverse a table with a large number of entries?
for example, given n=36 and entry size of 1 byte - what is the space / time used on a modern intel core to compute the merkle proof for the last (rightmost) item in the table?


