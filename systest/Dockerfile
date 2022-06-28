FROM golang:alpine3.15 as build
RUN apk add libc6-compat gcc musl-dev
WORKDIR /build/
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod go mod download
RUN --mount=type=cache,target=/root/.cache/go-build go test -v -c -o /build/tests.test ./systest/tests/

FROM alpine
COPY --from=build /build/tests.test /bin/tests