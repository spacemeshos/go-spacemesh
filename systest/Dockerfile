FROM golang:1.21 as build
RUN apt-get update -q
RUN apt-get install -qy ocl-icd-opencl-dev libpocl2 unzip

WORKDIR /build/

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN make get-postrs-service
RUN make get-postrs-lib
RUN make go-env-test

RUN --mount=type=cache,target=/root/.cache/go-build go test -failfast -v -c -o /build/tests.test ./systest/tests/

FROM debian:bookworm
RUN apt-get update -q
RUN apt-get install -qy libpocl2

COPY --from=build /build/tests.test /bin/tests
COPY --from=build /build/build/libpost.so /bin/libpost.so
COPY --from=build /build/build/service /bin/service
ENV LD_LIBRARY_PATH="/bin/"
ENV RUST_LOG=trace
