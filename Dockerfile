# go-spacemesh needs at least ubuntu 22.04. newer versions of ubuntu might work as well, but are untested
FROM ubuntu:22.04 AS linux
ENV DEBIAN_FRONTEND noninteractive
ENV SHELL /bin/bash
ARG TZ=Etc/UTC
ENV TZ=${TZ}
USER root
RUN set -ex \
   && apt-get update --fix-missing \
   && apt-get install -qy --no-install-recommends \
   ca-certificates \
   tzdata \
   locales \
   procps \
   net-tools \
   file \
   ocl-icd-libopencl1 clinfo \
   # required for OpenCL CPU provider
   # pocl-opencl-icd libpocl2 \
   && apt-get clean \
   && rm -rf /var/lib/apt/lists/* \
   && locale-gen en_US.UTF-8 \
   && update-locale LANG=en_US.UTF-8 \
   && echo "$TZ" > /etc/timezone
ENV LANG en_US.UTF-8
ENV LANGUAGE en_US.UTF-8
ENV LC_ALL en_US.UTF-8

FROM golang:1.22 as builder
ARG VERSION=""
ENV VERSION=${VERSION}
RUN set -ex \
   && apt-get update --fix-missing \
   && apt-get install -qy --no-install-recommends \
   unzip sudo \
   ocl-icd-opencl-dev

WORKDIR /src

COPY Makefile* .
RUN make get-libs

# We want to populate the module cache based on the go.{mod,sum} files.
COPY go.mod .
COPY go.sum .

RUN --mount=type=secret,id=mynetrc,dst=/root/.netrc go mod download

# Here we copy the rest of the source code
COPY . .

# And compile the project
RUN --mount=type=cache,id=build,target=/root/.cache/go-build make build VERSION=${VERSION}
RUN --mount=type=cache,id=build,target=/root/.cache/go-build make gen-p2p-identity
RUN --mount=type=cache,id=build,target=/root/.cache/go-build make merge-nodes VERSION=${VERSION}

# In this last stage, we start from a fresh image, to reduce the image size and not ship the Go compiler in our production artifacts.
FROM linux AS spacemesh

# Finally we copy the statically compiled Go binary.
COPY --from=builder /src/build/go-spacemesh /bin/
COPY --from=builder /src/build/service /bin/
COPY --from=builder /src/build/libpost.so /bin/
COPY --from=builder /src/build/gen-p2p-identity /bin/
COPY --from=builder /src/build/merge-nodes /bin/

ENTRYPOINT ["/bin/go-spacemesh"]
EXPOSE 7513

# profiling port
EXPOSE 6060

# pubsub port
EXPOSE 56565
