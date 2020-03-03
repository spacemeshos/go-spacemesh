<h1 align="center">
  <a href="https://spacemesh.io"><img width="400" src="https://spacemesh.io/content/images/2019/05/black_logo_hp.png" alt="Spacemesh logo" /></a>
  <p align="center">A programmable Cryptocurrency</p>
</h1>

<p align="center">

<a href="https://github.com/spacemeshos/go-spacemesh/blob/master/LICENSE"><img src="https://img.shields.io/packagist/l/doctrine/orm.svg"/></a>
<a href="https://github.com/avive"><img src="https://img.shields.io/badge/maintainer-%40avive-green.svg"/></a>
<img src="https://img.shields.io/badge/golang-%3E%3D%201.9.2-orange.svg"/>
<a href="https://gitter.im/spacemesh-os/Lobby"><img src="https://img.shields.io/badge/gitter-%23spacemesh--os-blue.svg"/></a>
<a href="https://spacemesh.io"><img src="https://img.shields.io/badge/madeby-spacemeshos-blue.svg"/></a>
[![Go Report Card](https://goreportcard.com/badge/github.com/spacemeshos/go-spacemesh)](https://goreportcard.com/report/github.com/spacemeshos/go-spacemesh)
[![Bors enabled](https://bors.tech/images/badge_small.svg)](https://app.bors.tech/repositories/22421)
<a href="https://godoc.org/github.com/spacemeshos/go-spacemesh"><img src="https://img.shields.io/badge/godoc-LGTM-blue.svg"/></a>
</p>
<p align="center">
<a href="https://gitcoin.co/profile/spacemeshos" title="Push Open Source Forward">
    <img src="https://gitcoin.co/static/v2/images/promo_buttons/slice_02.png" width="267px" height="52px" alt="Browse Gitcoin Bounties"/>
</a>
</p>

## go-spacemesh
💾⏰💪
Thanks for your interest in this open source project. This repo is the go implementation of the [Spacemesh](https://spacemesh.io) p2p full node software.

Spacemesh is a decentralized blockchain computer using a new race-free consensus protocol that doesn't involve energy-wasteful `proof of work`.

We aim to create a secure and scalable decentralized computer formed by a large number of desktop PCs at home.

We are designing and coding a modern blockchain platform from the ground up for scale, security and speed based on the learnings of the achievements and mistakes of previous projects in this space.

To learn more about Spacemesh head over to [https://spacemesh.io](https://spacemesh.io).

To learn more about the Spacemesh protocol [watch this video](https://www.youtube.com/watch?v=jvtHFOlA1GI).

### Motivation
Spacemesh is designed to create a decentralized blockchain smart contracts computer and a cryptocurrency that is formed by connecting the home PCs of people from around the world into one virtual computer without incurring massive energy waste and mining pools issues that are inherent in other blockchain computers, and provide a provably-secure and incentive-compatible smart contracts execution environment.

Spacemesh is designed to be ASIC-resistant and in a way that doesn’t give an unfair advantage to rich parties who can afford setting up dedicated computers on the network. We achieve this by using a novel consensus protocol and optimize the software to be most effectively be used on home PCs that are also used for interactive apps.

### What is this good for?
Provide dapp and app developers with a robust way to add value exchange and other value related features to their apps at scale. Our goal is to create a truly decentralized cryptocurrency that fulfills the original vision behind bitcoin to become a secure trustless store of value as well as a transactional currency with extremely low transaction fees.

### Target Users
go-spacemesh is designed to be installed and operated on users' home PCs to form one decentralized computer. It is going to be distributed in the Spacemesh App but people can also build and run it from source code.

### Project Status
We are working hard towards our first major milestone - a public permissionless testnet running the Spacemesh consensus protocol.

### Contributing
Thank you for considering to contribute to the go-spacemesh open source project!

We welcome contributions large and small and we actively accept contributions.

- go-spacemesh is part of [The Spacemesh open source project](https://spacemesh.io), and is MIT licensed open source software.

- We welcome collaborators to the Spacemesh core dev team.

- You don’t have to contribute code! Many important types of contributions are important for our project. See: [How to Contribute to Open Source?](https://opensource.guide/how-to-contribute/#what-it-means-to-contribute)

- To get started, please read our [contributions guidelines](https://github.com/spacemeshos/go-spacemesh/blob/master/CONTRIBUTING.md).

- Browse [Good First Issues](https://github.com/spacemeshos/go-spacemesh/labels/good%20first%20issue).

- Get ethereum awards for your contribution by working on one of our [gitcoin funded issues](https://gitcoin.co/profile/spacemeshos).

### Diggin' Deeper
Please read the Spacemesh [full FAQ](https://github.com/spacemeshos/go-spacemesh/wiki/Spacemesh-FAQ).

### go-spacemesh Architecture
![](https://raw.githubusercontent.com/spacemeshos/product/master/resources/go-spacemesh-architecture.png)

### High Level Design
![](https://raw.githubusercontent.com/spacemeshos/go-spacemesh/master/research/sp_arch_3.png)

### Client Software Architecture
![](https://raw.githubusercontent.com/spacemeshos/go-spacemesh/master/research/sm_arch_4.png)

### Getting

```bash
git clone git@github.com:spacemeshos/go-spacemesh.git
```
_-- or --_

Fork the project from https://github.com/spacemeshos/go-spacemesh

Since the project uses Go 1.11's Modules it is best to place the code **outside** your `$GOPATH`. Read [this](https://github.com/golang/go/wiki/Modules#how-to-install-and-activate-module-support) for alternatives.

### Setting Up Local Environment

Install [Go 1.11 or later](https://golang.org/dl/) for your platform, if you haven't already.

Ensure that `$GOPATH` is set correctly and that the `$GOPATH/bin` directory appears in `$PATH`.

Before building we need to install `protoc` (ProtoBuf compiler) and some tools required to generate ProtoBufs. Do this by running:
```bash
make install
```
This will invoke `setup_env.sh` which supports Linux and MacOS. On other platforms it should be straightforward to follow the steps in this script manually.


### Building
To build `go-spacemesh` for your current system architecture, from the project root directory, use:
```
make build
```

This will (re-)generate protobuf files and build the `go-spacemesh` binary, saving it in the `build/` directory.

To build a binary for a specific architecture directory use:
```
make darwin | linux | windows
```

Platform-specific binaries are saved to the `/build` directory.

---

### Running

go-spacemesh is p2p software which is designed to form a decentralized network by connecting to other instances of go-spacemesh running on remote computers.

To run go-spacemesh you need to specify the parameters shared between all instances on a specific network.

You specify these parameters by providing go-spacemesh with a toml config file. Other CLI flags control local node behavior and override default values.

#### Joining a Testnet (without mining)
1. Build go-spacemesh from source code.
2. Obtain the testnet's toml config file.
3. Start go-spacemesh with the following arguments:

```bash
./go-spacemesh --grpc-server --json-server --tcp-port [a_port] --config [tomlFileLocation] -d [nodeDataFilesPath]
```

##### Example
Assuming tn1.toml is a testnet config file saved in the same directory as go-spacemesh, use the following command to join the testnet. The data folder will be created in the same directory as go-spacemesh. The node will use TCP port 7152 and UDP port 7152 for p2p connections:

```bash
./go-spacemesh --grpc-server --json-server --tcp-port 7152 --config tn1.toml -d sm_data
```

4. Build the [CLI Wallet](https://github.com/spacemeshos/CLIWallet) from source code and run it:

5. Use the CLI Wallet commands to setup accounts, start smeshing and execute transactions.


```bash
./cli_wallet
```

#### Joining a Testnet (with mining)
1. Use the CLI Wallet to create a coinbase account.
2. Follow the steps for joining the testnet without mining but use these parameters when starting go-spacemesh:

```bash
./go-spacemesh --grpc-server --json-server --tcp-port [a_port] --config [tomlFileLocation] -d [nodeDataFilesPath] --coinbase [an_account] --start-mining --post-datadir [dir_for_post_data]
```

##### Example
```bash
./go-spacemesh --grpc-server --json-server --tcp-port 7152 --config tn1.toml -d sm_data --coinbase 0x36168c60e06abbb4f5df6d1dd6a1b15655d71e75 --start-mining --post-datadir post_data
```

3. Use the CLI wallet to check your coinbase account balance and to transact

#### Joining Spacemesh 0.1 ([TweedleDee](https://testnet.spacemesh.io/#/?id=what-is-spacemesh-01-tweedledee)) Testnet
- Build go-spacemesh source code from this github release: [go-spacemesh 0.1.3](https://github.com/spacemeshos/go-spacemesh/releases/tag/v0.1.3).
- Use [Testnet 0.1 (TweedleDee) Config File](https://storage.googleapis.com/smapp/0.0.3/tn01.toml).

---

### Testing

*NOTE*: if tests are hanging try running `ulimit -n 400`. some tests require that to work.

```
make test
```
or
```
make cover
```

### Docker
A `Dockerfile` is included in the project allowing anyone to build and run a docker image:
```bash
docker build -t spacemesh .
docker run -d --name=spacemesh spacemesh
```

### Windows
On windows you will need the following prerequisites:
- Powershell - included by in Windows by default since Windows 7 and Windows Server 2008 R2
- Git for Windows - after installation remove `C:\Program Files\Git\bin` from System PATH and add `C:\Program Files\Git\cmd` to System PATH
- Make - after installation add `C:\Program Files (x86)\GnuWin32\bin` to System PATH

You can then run the command `make install` followed by `make build` as on unix based systems.

### Running a Local Testnet
- You can run a local Spacemesh Testent with 6 full nodes, 6 user accounts, and 1 POET support service on your computer using docker.
- The local testnet full nodes are built from this repo.
- This is a great way to get a feel for the protocol and the platform and to start hacking on Spacemesh.
- Follow the steps in our [Local Testnet Guide](https://testnet.spacemesh.io/#/README)

#### Next Steps...
- Please visit our [wiki](https://github.com/spacemeshos/go-spacemesh/wiki)
- Browse project [go docs](https://godoc.org/github.com/spacemeshos/go-spacemesh)
- Spacemesh Protocol [video overview](https://www.youtube.com/watch?v=jvtHFOlA1GI)

### Got Questions?
- Introduce yourself and ask anything on the [spacemesh gitter channel](https://gitter.im/spacemesh-os/Lobby).
- DM [@teamspacemesh](https://twitter.com/teamspacemesh)
