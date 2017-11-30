<h1 align="center">
  <a href="https://unruly.io"><img width="400" src="https://firebasestorage.googleapis.com/v0/b/fifth-jigsaw-167200.appspot.com/o/logo%403x.png?alt=media&token=cdcacbe4-aa56-4111-b719-15b2ade60069" alt="Unruly logo" /></a>
</h1>


[![Packagist](https://img.shields.io/packagist/l/doctrine/orm.svg)]()
[![Maintainer](https://img.shields.io/badge/maintainer-%40avive-green.svg)]()
[![Goversion](https://img.shields.io/badge/golang-%3E%3D%201.9.2-orange.svg)]()



## go-unruly
The go implementation of the [UnrulyOS](https://unruly.io) p2p node.

### Build

Compile the .proto files using the protobufs go compiler:

```
cd pb
protoc --go_out=. ./*.proto
```
#### Vendoring 3rd party GO packages
We use [govendor](https://github.com/kardianos/govendor) for all 3rd party packages.
We commit to git all 3rd party packages in the vendor folder so we have our own copy of versioned releases.
To update a 3rd party package use vendor.json and govendor commands.

Installing govendor:
```
go get -u github.com/kardianos/govendor
```

To get the vendor packages use:
```
govendor init
govendor sync
```

To build the node use:

```
go build
```

### Running

```
./go-unruly
```

### Contributing

- go-unruly is part of [The UnrulyOS open source Project](https://unruly.io), and is MIT licensed open source software.
- We welcome contributions big and small! 
- We welcome major contributors to the unruly core dev team.
- Please make sure to scan the [issues](https://github.com/UnrulyOS/go-unruly/issues). 
- Search the closed ones before reporting things, and help us with the open ones.

#### Guidelines:

- Read the UnrulyOS project white paper
- Ask questions or talk about things in [Issues](https://github.com/UnrulyOS/go-unruly/issues) or #unruly on freenode.
- Ensure you are able to contribute (no legal issues please)
- For code contributions, fork the master branch, apply your changes and submit a pull request.
- Check for 3rd-party packages in the vendor folder before adding a new 3rd party dependency.
- Add new 3rd-party packages required by your code to vendor.json
- Squash your changes down to a single commit before submitting a PR and rebase on master so we can keep the commit timeline linear.
- Run `go fmt` before pushing any code
- Get in touch with @avive about how best to contribute
- Have fun hacking away our blockchain future!

There's a few things you can do right now to help out:
 - **check out existing issues**. This would be especially useful for modules in active development.
 - **Perform code reviews**.
 - **Add tests**. There can never be enough tests.

#### Toolschain

- Compiler - Please use go release 1.9.2. 
- Idea - We recommend GoLand but you can use your fave IDEA / text editor
- Debugger - We recommend delve and GoLand
- Testing - tbd
- Profiling - tbd


### Tests

### tasks

- Get rid of libp2p GX deps (asap) and them to vendor folder
- Support command line args in a robust way 
- Support basic account and keys ops

