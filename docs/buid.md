## Building

Build `go-unruly` in project root folder in your current arch:
```
make
```

Build a specific arch in the `/build` folder:

```
make linux | windows | darwin
```

## Build deps

- go 1.9.2 or later
- [govendor](https://github.com/kardianos/govendor)
- [protoc](https://github.com/golang/protobuf)
- go-protobufs and [grpc-gateway plugin](https://github.com/grpc-ecosystem/grpc-gateway) for `protoc-gen`:


```
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
go get -u github.com/golang/protobuf/protoc-gen-go
```