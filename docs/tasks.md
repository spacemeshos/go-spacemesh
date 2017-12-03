#### App Tasks / Issues

### App Shell
- Write tests for app flags, commands and config file
- Implement accounts and keystore files
- Implement node db in node data folders
- Basic working Makefile
- Impl cli.v1 TOML support and write test with TOML file (a bit of a pain with cli via vendoring)

#### Hard(er)
- Implement an optimized Merkle tree data structure with backing storage in leveldb (kv storage). Support Merkle proofs.
- Write tests to validate the plan to use libp2p-kad-dht for peer discovery (simulated network)
- add support for uTp (over udp) in lib-p2p - it only supports tcp right now.
- Implement gossip support for each protocol (gossip flag) and write tests for gossiping a p2p message (20 nodes, 1 bootstrap)
- Skeleton for [json-rpc] http://www.jsonrpc.org/specification and a Javascript console for an equiv javascript-rpc console. 
Json-rpc can be called from over the network, Javascript-rpc console locally.

