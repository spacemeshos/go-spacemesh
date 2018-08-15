FROM golang:1.9.2-alpine3.6 AS build

RUN apk add --no-cache make git

RUN go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
RUN go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
RUN go get -u github.com/golang/protobuf/protoc-gen-go
RUN go get -u github.com/spacemeshos/go-spacemesh
RUN go get -u github.com/kardianos/govendor
RUN cd src/github.com/spacemeshos/go-spacemesh; govendor sync; make
RUN cp /go/src/github.com/spacemeshos/go-spacemesh/config.toml /go

ENTRYPOINT /go/src/github.com/spacemeshos/go-spacemesh/go-spacemesh
EXPOSE 7513
