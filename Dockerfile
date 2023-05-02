# go-spacemesh needs at least ubuntu 20.04 (because gpu-post and post-rs are linked to glibc 2.31)
# newer versions of ubuntu should work as well, so far only 22.04 has been tested
FROM ubuntu:22.04 AS linux
ENV DEBIAN_FRONTEND noninteractive
ENV SHELL /bin/bash
ARG TZ=US/Eastern
ENV TZ $TZ
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

# alternative to installing ocl-icd-libopencl1, pocl-opencl-icd and libpocl2 is to install intel opencl driver
# RUN mkdir -p /tmp/opencl-driver-intel
# WORKDIR /tmp/opencl-driver-intel
# RUN apt-get update --fix-missing \
#    && apt-get install -qy --no-install-recommends wget gpg \
#    && wget -O- https://apt.repos.intel.com/intel-gpg-keys/GPG-PUB-KEY-INTEL-SW-PRODUCTS.PUB \
#    | gpg --dearmor | tee /usr/share/keyrings/oneapi-archive-keyring.gpg > /dev/null \
#    && echo "deb [signed-by=/usr/share/keyrings/oneapi-archive-keyring.gpg] https://apt.repos.intel.com/oneapi all main" | tee /etc/apt/sources.list.d/oneAPI.list \
#    && apt-get install -qy --no-install-recommends intel-oneapi-runtime-libs \
#    && apt-get clean \
#    && rm -rf /var/lib/apt/lists/* \
#    && rm -rf /tmp/opencl-driver-intel
# SHELL ["/bin/bash", "-c"]
# WORKDIR /
# RUN source /opt/intel/oneapi/lib/env/compiler_rt_vars.sh

FROM golang:1.19 as builder
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

RUN go mod download

# Here we copy the rest of the source code
COPY . .

# And compile the project
RUN --mount=type=cache,id=build,target=/root/.cache/go-build make build
RUN --mount=type=cache,id=build,target=/root/.cache/go-build make gen-p2p-identity

#In this last stage, we start from a fresh Alpine image, to reduce the image size and not ship the Go compiler in our production artifacts.
FROM linux AS spacemesh

# Finally we copy the statically compiled Go binary.
COPY --from=builder /src/build/go-spacemesh /bin/
COPY --from=builder /src/build/libgpu-setup.so /bin/
COPY --from=builder /src/build/libpost.so /bin/
COPY --from=builder /src/build/gen-p2p-identity /bin/

ENTRYPOINT ["/bin/go-spacemesh"]
EXPOSE 7513

# profiling port
EXPOSE 6060

# pubsub port
EXPOSE 56565
