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
- Should be working 


### Logging
- We are using []go-logging](https://github.com/op/go-logging)

### Config
- We are using TOML files for config file and cli.v1 flags to modify any configurable param via the CLI.


### Interactive Console
- TBD - Javascript console ???? do more research

#### App Shell Tasks

- Impl cli.v1 TOML support
- Write tests for app flags, commands and config file