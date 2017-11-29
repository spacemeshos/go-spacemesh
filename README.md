# go-unruly
UnrulyOS p2p node

## Build

Compile the .proto files using the protobufs go compiler:

```
cd pb
protoc --go_out=. ./*.proto
```

### Vendoring 3rd party GO packages
We use govendor https://github.com/kardianos/govendor for all 3rd party packages.
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

## Run

```
./go-unruly
```

## Tests

## tasks


- Get rid of libp2p GX deps (asap) and them to vendor folder
- Support command line args in a robust way 
- Support basic account and keys ops