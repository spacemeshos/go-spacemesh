FROM golang:1.22 as build
RUN set -ex \
    && apt-get update --fix-missing \
    && apt-get install -qy --no-install-recommends \
    unzip sudo \
    ocl-icd-opencl-dev

WORKDIR /src

COPY Makefile* .
RUN make get-libs
RUN make go-env-test

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .

RUN --mount=type=cache,id=build,target=/root/.cache/go-build go test -failfast -v -c -o ./build/tests.test ./systest/tests/

FROM ubuntu:22.04
RUN set -ex \
   && apt-get update --fix-missing \
   && apt-get install -qy --no-install-recommends \
   ocl-icd-libopencl1 clinfo \
   && apt-get clean \
   && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/build/tests.test /bin/tests
COPY --from=build /src/build/libpost.so /bin/libpost.so
COPY --from=build /src/build/service /bin/service
ENV LD_LIBRARY_PATH="/bin/"
