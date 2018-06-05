<h1 align="center">
  <a href="https://spacemesh.io"><img width="400" src="https://spacemesh.io/content/images/2018/05/logo-black-on-white-trimmed.png" alt="Spacemesh logo" /></a>
  <p align="center">Blockmesh Operating System</p>
</h1>

<p align="center">

<a href="https://github.com/spacemeshos/go-spacemesh/blob/master/LICENSE"><img src="https://img.shields.io/packagist/l/doctrine/orm.svg"/></a>
<a href="https://github.com/avive"><img src="https://img.shields.io/badge/maintainer-%40avive-green.svg"/></a>
<img src="https://img.shields.io/badge/golang-%3E%3D%201.9.2-orange.svg"/>
<a href="https://gitter.im/spacemesh-os/Lobby"><img src="https://img.shields.io/badge/gitter-%23spacemesh--os-blue.svg"/></a>
<a href="https://spacemesh.io"><img src="https://img.shields.io/badge/madeby-spacemeshos-blue.svg"/></a>
[![Go Report Card](https://goreportcard.com/badge/github.com/spacemeshos/go-spacemesh)](https://goreportcard.com/report/github.com/spacemeshos/go-spacemesh)
<a href="https://travis-ci.org/spacemeshos/go-spacemesh"><img src="https://api.travis-ci.org/spacemeshos/go-spacemesh.svg?branch=master" /></a>
<a href="https://godoc.org/github.com/spacemeshos/go-spacemesh"><img src="https://img.shields.io/badge/godoc-LGTM-blue.svg"/></a>
</p>
<p align="center">
<a href="https://gitcoin.co/profile/spacemeshos" title="Push Open Source Forward">
    <img src="https://gitcoin.co/static/v2/images/promo_buttons/slice_02.png" width="267px" height="52px" alt="Browse Gitcoin Bounties"/>
</a>
</p>

## go-spacemesh
💾⏰💪
Thanks for your interest in this open source project. This is the go implementation of the [Spacemesh](https://spacemesh.io) p2p node. Spacemesh is a decentralized blockchain computer using a new race-free consensus protocol that doesn't involve energy-wasteful `proof of work`. We aim to create a secure and scalable decentralized computer formed by a large number of desktop PCs at home. We are designing and coding a modern blockchain platform from the ground up for scale, security and speed based on the learnings of the achievements and mistakes of previous projects in this space. 

To learn more about Spacemesh head over to our [wiki](https://github.com/spacemeshos/go-spacemesh/wiki).

### Motivation
SpacemeshOS is designed to create a decentralized blockchain smart contracts computer and a cryptocurrency that is formed by connecting the home PCs of people from around the world into one virtual computer without incurring massive energy waste and mining pools issues that are inherent in other blockchain computers, and provide a provably-secure and incentive-compatible smart contracts execution environment. Spacemesh OS is designed to be ASIC-resistant and in a way that doesn’t give an unfair advantage to rich parties who can afford setting up dedicated computers on the network. We achieve this by using a novel consensus protocol and optimize the software to be most effectively be used on home PCs that are also used for interactive apps. 

### What is this good for?
Provide dapp and app developers with a robust way to add value exchange and other value related features to their apps at scale. Our goal is to create a truly decentralized cryptocoin that fulfills the original vision behind bitcoin to become a secure trustless store of value as well as a transactional currency with extremely low transaction fees.

### Target Users
go-spacemesh is designed to be installed and operated on users' home PCs to form one decentralized computer.

### Project Status
Development is currently focused on 3 main node core components:
1. The p2p/networking - the project includes a modern and robust p2p protocol for use by components up the stack.
2. The POST/blockmesh based consensus layer - Spacemesh protocol implementation, utilizing the p2p capabilities.  
3. App scaffolding - supporting functionality such as config, repl, cli and cross platform packaging.

Over the last few months, we had good progress on #1 and #3 and we are now starting to focus on #2.

### Contributing
Thank you for considering to contribute to the go-spacemesh open source project.  We welcome contributions large and small and we actively accept contributions.

- go-spacemesh is part of [The Spacemesh open source project](https://spacemesh.io), and is MIT licensed open source software.
- We welcome collaborators to the Spacemesh core dev team.
- You don’t have to contribute code! Many important types of contributions are important for our project. See: [How to Contribute to Open Source?](https://opensource.guide/how-to-contribute/#what-it-means-to-contribute)

- To get started, please read our [contributions guidelines](https://github.com/spacemeshos/go-spacemesh/blob/master/CONTRIBUTING.md).

- Browse [Good First Issues](https://github.com/spacemeshos/go-spacemesh/labels/good%20first%20issue).

#### NEW! Get crypto awarded for your contribution by working on one of our [gitcoin funded issues](https://gitcoin.co/profile/spacemeshos).

### Diggin' Deeper
Please read the Spacemesh [full FAQ](https://github.com/spacemeshos/go-spacemesh/wiki/Spacemesh-FAQ).

### High Level Design
![](https://raw.githubusercontent.com/spacemeshos/go-spacemesh/master/research/sp_arch_3.png)

### Client Software Architecture
![](https://raw.githubusercontent.com/spacemeshos/go-spacemesh/master/research/sm_arch_4.png)

### Getting

install [Go 1.9.2 or later](https://golang.org/dl/) for your platform

```
go get github.com/spacemeshos/go-spacemesh
```
or
- Fork the project from https://github.com/spacemeshos/go-spacemesh 
- Checkout the `develop` branch of your fork from GitHub
- Move your fork from `$GOPATH/src/github.com/YOURACCOUNT/go-spacemesh` to `$GOPATH/src/github.com/spacemeshos/go-spacemesh`
This allows GO tools to work as expected.

### Building
To build `go-spacemesh` for your current system architecture use:
```
make
```
or
```
go build
```
from the project root directory. The binary `go-spacemesh` will be saved in the project root directory.

To build a binary for a specific architecture directory use:
```
make darwin | linux | windows
```
Platform-specific binaries are saved to the `/build` directory.

### Running
```
./go-spacemesh
```

### Testing
```
make test
```
or 
```
make cover
```

#### Next Steps...
- Please visit our [wiki](https://github.com/spacemeshos/go-spacemesh/wiki)
- Browse project [go docs](https://godoc.org/github.com/spacemeshos/go-spacemesh)
- Spacemesh Protocol [first AMA session](https://spacemesh.io/tal-m-deep-dive/)

### Got Questions? 
- Introduce yourself and ask anything on the [spacemesh gitter channel](https://gitter.im/spacemesh-os/Lobby).
- DM [@teamspacemesh](https://twitter.com/teamspacemesh)
