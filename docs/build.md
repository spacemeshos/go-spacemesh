## Building

Build `go-spacemesh` in project root folder in your current arch:
```
make
```

Build a specific arch in the `/build` folder:

```
make linux | windows | darwin
```

## Build Requirements
- go 1.9.2 or later


## Build tools
Install these when making code changes

- [govendor](https://github.com/kardianos/govendor) - for adding a 3rd party dep
- [protoc](https://github.com/golang/protobuf) - for working with `protobufs`
- [grpc-gateway plugin](https://github.com/grpc-ecosystem/grpc-gateway) for `protoc` - when working on the api servers.



```
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
go get -u github.com/golang/protobuf/protoc-gen-go
```