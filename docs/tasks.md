#### App Tasks / Issues

### Build tasks
- Dockerfile fo a completely isolated docker dev env
- Makefile that builds node packaged binary without relying on packages outside of /vendor

### App Shell Tasks
- Write tests for app flags, commands and config file
- Implement accounts and keystore files
- Implement node db in node data folders
- Basic working Makefile
- Impl cli.v1 TOML support and write test with TOML file (a bit of a pain with cli via vendoring)

#### Misc. Tasks

- Implement an optimized Merkle tree data structure with backing storage in leveldb (kv storage). Support Merkle proofs.
- Write tests to validate the plan to use libp2p-kad-dht for peer discovery (simulated network)
- add support for uTp (over udp) in lib-p2p - it only supports tcp right now.
- Implement gossip support for each protocol (gossip flag) and write tests for gossiping a p2p message (20 nodes, 1 bootstrap)
- [gRPC](https://grpc.io/docs/tutorials/basic/go.html)-based RPC API. REST-JSON [generated gateway](https://github.com/grpc-ecosystem/grpc-gateway). Local Javscript console for interactive RPC calls locally.
 
