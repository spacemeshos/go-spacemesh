# libsvm.a is linked with fcntl64, which was added to glibc since 2.28.
# The earliest Ubuntu release that has glibc >=2.28 by default is 18.10, so the LTS one is 20.04.
# If any issues occur on 20.04, it may be downgraded back to 18.04 with manual glibc upgrade.
FROM ubuntu:20.04 AS linux
ENV DEBIAN_FRONTEND noninteractive
ENV SHELL /bin/bash
ARG TZ=US/Eastern
ENV TZ $TZ
USER root
RUN bash -c "for i in {1..9}; do mkdir -p /usr/share/man/man\$i; done" \
 && echo 'APT::Get::Assume-Yes "true";' > /etc/apt/apt.conf.d/90noninteractive \
 && echo 'DPkg::Options "--force-confnew";' >> /etc/apt/apt.conf.d/90noninteractive \
 && apt-get update --fix-missing \
 && apt-get install -qy --no-install-recommends \
    ca-certificates \
    tzdata \
    locales \
    procps \
    net-tools \
    apt-transport-https \
    file \
    # -- it allows to start with nvidia-docker runtime --
    #libnvidia-compute-390 \
 && apt-get clean \
 && rm -rf /var/lib/apt/lists/* \
 && locale-gen en_US.UTF-8 \
 && update-locale LANG=en_US.UTF-8 \
 && echo "$TZ" > /etc/timezone
ENV LANG en_US.UTF-8
ENV LANGUAGE en_US.UTF-8
ENV LC_ALL en_US.UTF-8
ENV NVIDIA_REQUIRE_CUDA "cuda>=9.1 driver>=390"
ENV NVIDIA_VISIBLE_DEVICES all
ENV NVIDIA_DRIVER_CAPABILITIES compute,utility,display
LABEL com.nvidia.volumes.needed="nvidia_driver"

FROM linux as golang
ARG TARGETPLATFORM
ENV GOLANG_MAJOR_VERSION 1
ENV GOLANG_MINOR_VERSION 18
ENV GOLANG_PATCH_VERSION 1
ENV GOLANG_VERSION $GOLANG_MAJOR_VERSION.$GOLANG_MINOR_VERSION.$GOLANG_PATCH_VERSION
ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH
RUN set -ex \
 && apt-get update --fix-missing \
 && apt-get install -qy --no-install-recommends \
    gcc \
	libc6-dev \
    git \
    bash \
    sudo \
    unzip \
    make \
    curl \
 && apt-get clean \
 && rm -rf /var/lib/apt/lists/* \
 && if [ "${TARGETPLATFORM}" = "linux/arm64" ]; then export ARCHITECTURE=arm64; else export ARCHITECTURE=amd64; fi \
 && curl -L https://golang.org/dl/go${GOLANG_VERSION}.linux-${ARCHITECTURE}.tar.gz | tar zx -C /usr/local \
 && go version \
 && mkdir -p "$GOPATH/src" "$GOPATH/bin" \
 && chmod -R 777 "$GOPATH"

FROM golang as build_base
WORKDIR /go/src/github.com/spacemeshos/go-spacemesh

# Force the go compiler to use modules
ENV GO111MODULE=on
ENV GOPROXY=https://proxy.golang.org

# We want to populate the module cache based on the go.{mod,sum} files.
COPY go.mod .
COPY go.sum .
COPY scripts/* scripts/

# does not required yet
# RUN go run scripts/check-go-version.go --major 1 --minor 15
RUN	go mod download
RUN GO111MODULE=off go get golang.org/x/lint/golint

# This image builds the go-spacemesh server
FROM build_base AS server_builder
# Here we copy the rest of the source code
COPY . .

# And compile the project
RUN make build
RUN make harness
RUN make gen-p2p-identity

#In this last stage, we start from a fresh Alpine image, to reduce the image size and not ship the Go compiler in our production artifacts.
FROM linux AS spacemesh

# Finally we copy the statically compiled Go binary.
COPY --from=server_builder /go/src/github.com/spacemeshos/go-spacemesh/build/go-spacemesh /bin/
COPY --from=server_builder /go/src/github.com/spacemeshos/go-spacemesh/build/go-harness /bin/
COPY --from=server_builder /go/src/github.com/spacemeshos/go-spacemesh/build/libgpu-setup.so /bin/
# TODO(nkryuchkov): uncomment when go-svm is imported
#COPY --from=server_builder /go/src/github.com/spacemeshos/go-spacemesh/build/libsvm.so /bin/
COPY --from=server_builder /go/src/github.com/spacemeshos/go-spacemesh/build/gen-p2p-identity /bin/

ENTRYPOINT ["/bin/go-harness"]
EXPOSE 7513

# profiling port
EXPOSE 6060

# pubsub port
EXPOSE 56565
