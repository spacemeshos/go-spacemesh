#### App Tasks / Issues

- All tasks moved to [Github Issues](https://github.com/spacemeshos/go-spacemesh/issues)
- All new tasks should be added to github as issues.
- This is a list of partly or fully implemented features.

### App Shell Implemented Features
Features we have a basic 1st pass implementation for:
- Initial project packages structure and entry points (app, node, grpcApi, etc...)
- A cli app shell with flags and commands
- Grpc and json-http api servers with config and decent tests
- Basic node with 2 implemented example p2p protocols and basic tests
- Basic keys and id type system (wrapping lib-p2p keys and ids types)
- A buildable go-spacemesh cross-platform exe with full cli features (flags, commands)
- Basic working makefile (not fully isolated from $GOPATH although make should use vendored versions)
- App persistent store with support for multiple nodes data
- Restore node data from store on session start or create new id automatically
- Auto persist node data to store
- Load data about bootstrap nodes on node startup
- Integrated rolling file logging https://github.com/natefinch/lumberjack with go-logging so we get proper rolling files logging - needed for long-running simulations.
- Accounts support with locking/unlocking, persistence and passpharse kdf
- Implement accounts and keystore files
- Per-node logger to console and to node-specific log files

## p2p Implemented Features
- Swarm - peer management, low-level tcp network and connections
- Partly done: node discovery protocol - connect to bootstrap node and to 5 random nodes on startup
- Handshake protocol for secure and authenticated comm using a ECDH key exchange scheme
- App-level p2p protocols support with full encryption and authorship auth - see ping
- Pattern for app-level protocols using callback channels for processing responses to requests
- Design for an extendable protobufs based message format - see message.proto
- Demuxer (router) - routes incoming messages to app-level protocols handlers
- 100% mutex-free (mutexes in msgio should be removed as well) - all concurrent flows are using channels
- 100% lib-p2p free :-)
- Basic DHT and findPeer protocol
