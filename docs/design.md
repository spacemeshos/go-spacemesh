## App Design

### p2p, accounts, keys and nodes
- Please refer to p2p.md

### App Shell
- We are using https://gopkg.in/urfave/cli.v1 for the CLI app shell

### Runtime Metrics
- We are using https://github.com/rcrowley/go-metrics for system metrics

### Local DB
- We are using goleveldb for a key-value db

### Debugging

#### GoLand IDEA interactive debugging
- Should be fully working - create an IDEA config to run main in the proj root source dir

### Logging
- We are using [go-logging](https://github.com/op/go-logging)
- Use /log/logger funcs for app-level logging
- You may add custom loggers

### Config
- We are using TOML files for config file and cli.v1 flags to modify any configurable param via the CLI.

### Interactive Console
- TBD - Javascript console ???? do more research

#### App Tasks / Issues
- Impl cli.v1 TOML support and write test with TOML file
- Write tests for app flags, commands and config file
- Write tests to validate the plan to use libp2p-kad-dht for peer discovery
- Implement gossip protocol flag and write tests for gossiping a p2p message