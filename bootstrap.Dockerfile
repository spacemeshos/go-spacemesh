FROM golang:1.19 as builder

WORKDIR /src

COPY Makefile* .
COPY go.mod .
COPY go.sum .

RUN go mod download

# copy the rest of the source code
COPY . .

# compile
RUN --mount=type=cache,id=build,target=/root/.cache/go-build make bootstrapper

# start from a fresh Alpine image with just the built binary
FROM ubuntu:22.04

COPY --from=builder /src/build/go-bootstrapper /bin/

ENTRYPOINT ["/bin/go-bootstrapper"]