
<h1 align="center">
  <a href="https://spacemesh.io"><img width="400" src="https://firebasestorage.googleapis.com/v0/b/dromo-os.appspot.com/o/spacemesh-logo.png?alt=media&token=dcd60c71-8522-4e02-9bc2-e439f89577f2" alt="Spacemesh logo" /></a>
</h1>

<p align="center">

<img src="https://img.shields.io/packagist/l/doctrine/orm.svg"/>
<a href="https://github.com/avive"><img src="https://img.shields.io/badge/maintainer-%40avive-green.svg"/></a>
<img src="https://img.shields.io/badge/golang-%3E%3D%201.9.2-orange.svg"/>
<a href="https://gitter.im/spacemesh-os/Lobby"><img src="https://img.shields.io/badge/gitter-%23spacemesh--os-blue.svg"/></a>
<a href="https://spacemesh.io"><img src="https://img.shields.io/badge/madeby-spacemeshos-blue.svg"/></a>
<img src="https://api.travis-ci.org/spacemeshos/go-spacemesh.svg?branch=master"/>
</p>

## go-spacemesh

The go implementation of the [Spacemesh os](https://spacemesh.io) p2p node.
Spacemesh is a decentralized blockchain computer using a new race-free consensus protocol that doesn't involve `proof of work`.
Spacemesh is designed to build a secure decentralized network formed by a large number of desktop PCs at home.
To learn more about SpaceMesh read our wiki pages.

### Getting
```
go get https://github.com/spacemeshos/go-spacemesh
```
or create this directory `GOPATH$/src/spacemeshos/` and clone the repo into it using:

```
git clone https://github.com/spacemeshos/go-spacemesh
```

### Building


#### Step 1 - Install the build tools

Install these go tools:
- [Govendor](https://github.com/kardianos/govendor) - for managing 3rd party deps
- [Protoc](https://github.com/golang/protobuf) - for compiling `protobufs`
- [Grpc-gateway plugin](https://github.com/grpc-ecosystem/grpc-gateway) - `protoc` support for grpc json-httpproxy 
- [Go 1.9.2 or later](https://golang.org/dl/)
```
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
go get -u github.com/golang/protobuf/protoc-gen-go
```

#### Step 2 - Get the dependencies

We use [govendor](https://github.com/kardianos/govendor) for all 3rd party packages.
We commit to git all 3rd party packages in the `vendor` folder so we have our own copy of versioned releases.
To update a 3rd party package use vendor.json and govendor commands.

```
go get -u github.com/kardianos/govendor
govendor sync
```

#### Step 3 - Build

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


Compile the .proto files using the protobufs go compiler:

```
cd pb
protoc --go_out=. ./*.proto
```

### Running

```
./go-spacemesh
```

### Testing
```
./go test ./...
```
or
```
make test
```
### Contributing

Thank you for considering to contribute to the go-spacemesh open source project. 
We welcome contributions large and small!

- go-spacemesh is part of [The Spacemesh open source project](https://spacemesh.io), and is MIT licensed open source software.
- We welcome major contributors to the spacemesh core dev team.
- Please make sure to scan the [issues](https://github.com/spacemeshos/go-spacemesh/issues). 
- Search the closed ones before reporting things, and help us with the open ones.

#### Guidelines

- Read the Spacemesh project white paper (coming soon)
- Read all docs in the `docs/` folder
- Ask questions or talk about things in [Issues](https://github.com/spacemeshos/go-spacemesh/issues) or on [spacemash gitter](https://gitter.im/spacemesh-os/Lobby).
- Ensure you are able to contribute (no legal issues please)
- For any code contribution, please **fork from the `develop branch` and not from `master`), apply your changes and submit a pull request. We follow this [git workflow](http://nvie.com/posts/a-successful-git-branching-model/)
- Before starting to work on large contributions please chat with the core dev team on our [gitter channel](https://gitter.im/spacemesh-os/Lobby) to get some initial feedback prior to doing lots of work.
- Check for 3rd-party packages in the vendor folder before adding a new 3rd party dependency.
- Add new 3rd-party packages required by your code to vendor.json - don't use any other kind of deps importing.
- Add the package name to your commit comment. e.g. `node: added additional tests.`
- Squash your changes down to a single commit before submitting a PR and rebase on master so we can keep the commit timeline linear.
- We adhere to the go standard formatting. Run `go fmt` before pushing any code
- Get in touch with @avive about how best to contribute
- Your code must be commented using [go commentary](https://golang.org/doc/effective_go.html#commentary)
- **Have fun hacking away our blockchain future!**

There's a few things you can do right now to help out:
 - Check out existing [open issues](https://github.com/spacemeshos/go-spacemesh/issues). This would be especially useful for modules in active development.
 - Add tests. There can never be enough tests.
 
#### Next Steps...
- Please read everything in [`/docs`](https://github.com/spacemeshos/go-spacemesh/tree/master/docs)

