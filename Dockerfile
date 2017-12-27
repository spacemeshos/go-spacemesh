# FROM golang:onbuild
FROM golang:1.9.2-alpine3.6 AS build

RUN apk add --no-cache make git

ADD . /go/src/github.com/spacemeshos/go-spacemesh
RUN cd /go/src/github.com/spacemeshos/go-spacemesh && make
ENTRYPOINT /go/src/github.com/spacemeshos/go-spacemesh/go-spacemesh
EXPOSE 7513
