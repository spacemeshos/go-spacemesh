#### App Tasks / Issues

### App Shell
- Impl cli.v1 TOML support and write test with TOML file (a bit of a pain with cli via vendoring)
- Write tests for app flags, commands and config file
- Implement accounts and keystore files
- Implement node db in node data folders
- Basic working Makefile
- Implement an optimized Markle Trie data structure with backing storage in leveldb (kv storage)

#### Hard
- Write tests to validate the plan to use libp2p-kad-dht for peer discovery (simulated network)
- add support for uTp (over udp) in lib-p2p - it only supports tcp right now.
- Implement gossip support for each protocol (gossip flag) and write tests for gossiping a p2p message (20 nodes, 1 bootstrap)

