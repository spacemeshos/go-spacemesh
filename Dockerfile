# libsvm.a is linked with fcntl64, which was added to glibc since 2.28.
# The earliest Ubuntu release that has glibc >=2.28 by default is 18.10, so the LTS one is 20.04.
# If any issues occur on 20.04, it may be downgraded back to 18.04 with manual glibc upgrade.
FROM ubuntu:23.04 AS linux
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

FROM golang:1.19 as builder
RUN set -ex \
   && apt-get update --fix-missing \
   && apt-get install -qy --no-install-recommends \
   unzip

WORKDIR /src

COPY Makefile* .
RUN make get-libs

# We want to populate the module cache based on the go.{mod,sum} files.
COPY go.mod .
COPY go.sum .

RUN go mod download

# Here we copy the rest of the source code
COPY . .

# And compile the project
RUN --mount=type=cache,id=build,target=/root/.cache/go-build make build
RUN --mount=type=cache,id=build,target=/root/.cache/go-build make harness
RUN --mount=type=cache,id=build,target=/root/.cache/go-build make gen-p2p-identity

#In this last stage, we start from a fresh Alpine image, to reduce the image size and not ship the Go compiler in our production artifacts.
FROM linux AS spacemesh

# Finally we copy the statically compiled Go binary.
COPY --from=builder /src/build/go-spacemesh /bin/
COPY --from=builder /src/build/go-harness /bin/
COPY --from=builder /src/build/libgpu-setup.so /bin/
# TODO(nkryuchkov): uncomment when go-svm is imported
#COPY --from=builder /src/build/libsvm.so /bin/
COPY --from=builder /src/build/gen-p2p-identity /bin/

ENTRYPOINT ["/bin/go-harness"]
EXPOSE 7513

# profiling port
EXPOSE 6060

# pubsub port
EXPOSE 56565
