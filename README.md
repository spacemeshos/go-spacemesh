# go-unruly
UnrulyOS p2p node

## Build

Compile the .proto files using the protobufs go compiler:

```
cd pb
protoc --go_out=. ./*.proto
```

### 3rd party GO packages
We use govendor https://github.com/kardianos/govendor
To install it use:

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

## tasks


- Get rid of libp2p GX deps (asap)
- Support command line args in a robust way 
- Support basic account and keys ops