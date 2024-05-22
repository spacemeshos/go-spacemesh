FROM golang:1.22 as builder

WORKDIR /src

COPY Makefile* .
COPY go.mod .
COPY go.sum .

RUN --mount=type=secret,id=mynetrc,dst=/root/.netrc go mod download

# copy the rest of the source code
COPY . .

# compile
RUN --mount=type=cache,id=build,target=/root/.cache/go-build make bootstrapper

# start from a fresh image with just the built binary
FROM ubuntu:22.04

RUN apt-get update && apt-get install ca-certificates -y

COPY --from=builder /src/build/go-bootstrapper /bin/

ENTRYPOINT ["/bin/go-bootstrapper"]
