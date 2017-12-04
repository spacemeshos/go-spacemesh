#### App Tasks / Issues

### Build tasks
- Dockerfile to setup a completely isolated docker dev env
- Makefile that builds node packaged binary without relying on packages outside of /vendor - e.g. GOPATH as proj root
- Script to install required build tools: e.g. proto-c (with grpc gateway support). govendor, etc....
- Integrate travis CI with the repo and setup full CI system

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
- Local Javscript console for interactive RPC calls (wrap json http api).
- Tests for all implement functionality - the more the better.


#### In development
- REST-JSON [generated gateway](https://github.com/grpc-ecosystem/grpc-gateway). 

 
