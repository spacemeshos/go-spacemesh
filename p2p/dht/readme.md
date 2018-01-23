## SPACEMESH-DHT
In this package we'd like to implement a modern [DHT](https://en.wikipedia.org/wiki/Distributed_hash_table) with the following main properties:

1. All comm between nodes is secure and authenticated (using spacemesh-swarm). This is sometimes known as s-kademlia.
2. [Coral](https://en.wikipedia.org/wiki/Coral_Content_Distribution_Network) improvements to Kademlia content distribution. aka [mainlin dht](https://en.wikipedia.org/wiki/Mainline_DHT). 
Implemented in libp2p-kad-dht and bittorrent clients
3. Avoid the complexity and possible race conditions involved in `go-eth-p2p-kad` and `libp2p-kad-dht`. 
libp2p open-issues list is somewhat scary and libp2p-kad-dht is tightly coupled with its main client - ipfs.
4. Have as little external deps as possible - e.g. multi-address, specific key-id schemes, etc...

This package contains heavily-modified code inspired by `libp2p-kad-dht` and `libp2p-kbucket`. The license file for both of these packages is included in this package as required by the MIT licensing terms.

## DHT k-buckets fun facts
- About 50% of peers observed by local node should be stored in the table.
- The lower the bucket, the closer the peers are to the local node
- The first bucket contains nodes that share 1 msb bit with the local node
- Nodes may be dropped from the table if their buckets are getting full based on how far ago the local node communicated with them or learned about them.
