<h1 align="center">
  <a href="https://spacemesh.io"><img width="400" src="https://firebasestorage.googleapis.com/v0/b/dromo-os.appspot.com/o/spacemesh-logo.png?alt=media&token=dcd60c71-8522-4e02-9bc2-e439f89577f2" alt="Spacemesh logo" /></a>
  <p align="center">Make blockchain decentralized again™</p>
</h1>

<p align="center">

<a href="https://github.com/spacemeshos/go-spacemesh/blob/master/LICENSE"><img src="https://img.shields.io/packagist/l/doctrine/orm.svg"/></a>
<a href="https://github.com/avive"><img src="https://img.shields.io/badge/maintainer-%40avive-green.svg"/></a>
<img src="https://img.shields.io/badge/golang-%3E%3D%201.9.2-orange.svg"/>
<a href="https://gitter.im/spacemesh-os/Lobby"><img src="https://img.shields.io/badge/gitter-%23spacemesh--os-blue.svg"/></a>
<a href="https://spacemesh.io"><img src="https://img.shields.io/badge/madeby-spacemeshos-blue.svg"/></a>
[![Go Report Card](https://goreportcard.com/badge/github.com/spacemeshos/go-spacemesh)](https://goreportcard.com/report/github.com/spacemeshos/go-spacemesh)
<a href="https://travis-ci.org/spacemeshos/go-spacemesh"><img src="https://api.travis-ci.org/spacemeshos/go-spacemesh.svg?branch=master"/></a>
<a href="https://godoc.org/github.com/spacemeshos/go-spacemesh"><img src="https://img.shields.io/badge/godoc-LGTM-blue.svg"/></a>
</p>

## go-spacemesh
Thanks for your interest in this open source project. This is the go implementation of the [Spacemesh](https://spacemesh.io) p2p node. Spacemesh is a decentralized blockchain computer using a new race-free consensus protocol that doesn't involve energy-wasteful `proof of work`. We aim to create a secure and scalable decentralized computer formed by a large number of desktop PCs at home. We are designing and coding a modern blockchain platform from the ground up for scale, security. and speed based on the learnings, achievements and mistakes of previous projects in this space. 

To learn more about Spacemesh head over to our [wiki](https://github.com/spacemeshos/go-spacemesh/wiki).

### Motivation
SpacemeshOS is designed to create a decentralized blockchain smart contracts computer and a cryptocurrency that is formed by connecting the home PCs of people from around the world into one virtual computer without incurring massive energy waste and mining pools issues that are inherent in other blockchain computers, and provide a provably-secure and incentive-compatible smart contracts execution environment. Spacemesh OS is designed to be ASIC-resistant and in a way that doesn’t give an unfair advantage to rich parties who can afford setting up dedicated computers on the network. We achieve this by using a novel consensus protocol and optimize the software to be most effectively be used on home PCs that are also used for interactive apps. 

### What is this good for?
Provide dapp and app developers with a robust way to add value exchange and value related features to their apps at scale. Our goal is to create a truly decentralized cryptocoin that fulfills the original vision behind bitcoin to become a secure trustless store of value as well as a transactional currency with extremely low transaction fees.

### Diggin' Deeper
Please read the Spacemesh [full FAQ](https://github.com/spacemeshos/go-spacemesh/wiki/Spacemesh-FAQ).

### Getting

install [Go 1.9.2 or later](https://golang.org/dl/) for your platform

```
go get github.com/spacemeshos/go-spacemesh
```
or
- Fork https://github.com/spacemeshos/go-spacemesh project
- Checkout your fork from GitHub
- Move your fork from $GOPATH/src/github.com/YOURACCOUNT/go-spacemesh to $GOPATH/src/github.com/spacemeshos/go-spacemesh
This allows Go tools to work as expected.

```
git clone https://github.com/spacemeshos/go-spacemesh
```

Next, get the dependencies using [govendor](https://github.com/kardianos/govendor). Run this from the project root directory:

```
go get -u github.com/kardianos/govendor
govendor sync
```

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
go test ./...
```
or
```
make test
```
### Contributing

Thank you for considering to contribute to the go-spacemesh open source project. 
We welcome contributions large and small and we actively accept contributions.
- go-spacemesh is part of [The Spacemesh open source project](https://spacemesh.io), and is MIT licensed open source software.
- We welcome major contributors to the spacemesh core dev team.
- Please make sure to scan the [issues](https://github.com/spacemeshos/go-spacemesh/issues). 
- Search the closed ones before reporting things, and help us with the open ones.
- You don’t have to contribute code! Many important types of contributions are important for our project. See: [How to Contribute to Open Source?](https://opensource.guide/how-to-contribute/#what-it-means-to-contribute)?

#### Guidelines

- Read the Spacemesh project white paper (coming soon)
- Ask questions or talk about things in [Issues](https://github.com/spacemeshos/go-spacemesh/issues) or on [spacemash gitter](https://gitter.im/spacemesh-os/Lobby).
- Ensure you are able to contribute (no legal issues please)
- For any code contribution, please fork from the `develop branch` and not from `master`), apply your changes and submit a pull request. We follow this [git workflow](http://nvie.com/posts/a-successful-git-branching-model/)
- Before starting to work on large contributions please chat with the core dev team on our [gitter channel](https://gitter.im/spacemesh-os/Lobby) to get some initial feedback prior to doing lots of work.
- Check for 3rd-party packages in the vendor folder before adding a new 3rd party dependency.
- Add new 3rd-party packages required by your code to vendor.json - don't use any other kind of deps importing.
- Add the package name to your commit comment. e.g. `node: added additional tests.`
- Squash your changes down to a single commit before submitting a PR and rebase on master so we can keep the commit timeline linear.
- We adhere to the go standard formatting. Run `go fmt` before pushing any code
- Get in touch with @avive about how best to contribute
- Your code must be commented using [go commentary](https://golang.org/doc/effective_go.html#commentary)
- You can add your your name and email to AUTHORS when submitting a Pull Request.

- **Have fun hacking away our blockchain future!**

Few things you can do right now to help out:
 - Check out existing [open issues](https://github.com/spacemeshos/go-spacemesh/issues). This would be especially useful for modules in active development.
 - Add tests. There can never be enough tests.
 
#### Next Steps...
- Please scan our [`wiki`](https://github.com/spacemeshos/go-spacemesh/wiki)
- Browse project [go docs](https://godoc.org/github.com/spacemeshos/go-spacemesh)

### Got Questions? 
- Introduce yourself and ask anything on the [spacemesh gitter channel](https://gitter.im/spacemesh-os/Lobby).
- DM [@spacemeshhq](https://twitter.com/teamspacemesh)

### Additional info

#### Working with Dependencies

We use [govendor](https://github.com/kardianos/govendor) for all 3rd party packages.
We commit to git all 3rd party packages in the `vendor` folder so we have our own copy of versioned releases.
To update a 3rd party package use vendor.json and govendor commands.

#### Working with protobufs

Install:
- [Protoc with Go support](https://github.com/golang/protobuf) - for compiling `protobufs`
- [Grpc-gateway plugin](https://github.com/grpc-ecosystem/grpc-gateway) - `protoc` support for grpc json-httpproxy 

```
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
go get -u github.com/golang/protobuf/protoc-gen-go
```

To compile .proto files you need to install the [protobufs go support](https://github.com/golang/protobuf)
After installing. You can compile a .proto file using:

```
cd pb
protoc --go_out=. ./*.proto
```
