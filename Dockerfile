# Inspired by https://container-solutions.com/faster-builds-in-docker-with-go-1-11/
# Base build image
FROM golang:1.15.13-alpine3.12 AS build_base
RUN apk add bash make git curl unzip rsync libc6-compat gcc musl-dev lsof
WORKDIR /go/src/github.com/spacemeshos/go-spacemesh

# Force the go compiler to use modules
ENV GO111MODULE=on
ENV GOPROXY=https://proxy.golang.org

# We want to populate the module cache based on the go.{mod,sum} files.
COPY go.mod .
COPY go.sum .

# Download dependencies
RUN go mod download

COPY setup_env.sh .
COPY scripts/* scripts/

RUN ./setup_env.sh

RUN go get github.com/golang/snappy@v0.0.1

# This image builds the go-spacemesh server
FROM build_base AS server_builder
# Here we copy the rest of the source code
COPY . .

# And compile the project
RUN make build
RUN make hare
RUN make p2p
RUN make sync
RUN make harness

#In this last stage, we start from a fresh Alpine image, to reduce the image size and not ship the Go compiler in our production artifacts.
FROM alpine AS spacemesh

# Finally we copy the statically compiled Go binary.
COPY --from=server_builder /go/src/github.com/spacemeshos/go-spacemesh/build/go-spacemesh /bin/go-spacemesh
COPY --from=server_builder /go/src/github.com/spacemeshos/go-spacemesh/build/go-hare /bin/go-hare
COPY --from=server_builder /go/src/github.com/spacemeshos/go-spacemesh/build/go-p2p /bin/go-p2p
COPY --from=server_builder /go/src/github.com/spacemeshos/go-spacemesh/build/go-sync /bin/go-sync
COPY --from=server_builder /go/src/github.com/spacemeshos/go-spacemesh/build/go-harness /bin/go-harness

ENTRYPOINT ["/bin/go-harness"]
EXPOSE 7513

# profiling port
EXPOSE 6060

# pubsub port
EXPOSE 56565
