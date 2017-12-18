## UNRULY-DHT
In this package we'd like to implement a modern [DHT](https://en.wikipedia.org/wiki/Distributed_hash_table) with the following main properties:

1. All comm between nodes is secure and authenticated (using unruly-swarm). This is sometimes known as s-kademlia.
2. [Coral](https://en.wikipedia.org/wiki/Coral_Content_Distribution_Network) improvements to Kademlia content distribution. aka [mainlin dht](https://en.wikipedia.org/wiki/Mainline_DHT). 
Implemented in libp2p-kad-dht and bittorent clients
3. Avoid the complexity and possible race conditions involved in `go-eth-p2p-kad` and `libp2p-kad-dht`. 
libp2p open-issues list is somewhat scray and libp2p-kad-dht is tightly coupled with its main client - ipfs.
4. Have as little external deps as possible - e.g. multi-address, specific key-id schemes, etc...


### Design notes

- The keyspace package adapted from libp2p-kad-dht. We like all libp2p-* packages that don't assume peer id and key format and don't have external deps.
