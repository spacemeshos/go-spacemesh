# go-unruly
UnrulyOS p2p node

## Build

Compile the .proto files using the protobufs go compiler:

```
cd pb
protoc --go_out=. ./*.proto
```

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