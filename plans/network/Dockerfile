# This Dockerfile performs a multi-stage build and RUNTIME_IMAGE is the image
# onto which to copy the resulting binary.
#
# Picking a different runtime base image from the build image allows us to
# slim down the deployable considerably.
#
# The user can override the runtime image by passing in the appropriate builder
# configuration option.
ARG run_image=debian:stable
ARG build_image=golang:1.16-buster
#:::
#::: BUILD CONTAINER
#:::
FROM ${build_image} AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
		unzip

# COPY extra/go-spacemesh/[a-z]* /go-spacemesh/
COPY extra/go-spacemesh /go-spacemesh
COPY plan/go.mod /tmp/go.mod

ENV TESTGROUND_PLAN=1
#RUN cp /go-spacemesh/Makefile.Inc /go-spacemesh/plans/`awk '/^module/ {split($0,a,"/"); print a[5]}' /tmp/go.mod`/Makefile.Inc
RUN cd /go-spacemesh/plans/`awk '/^module/ {split($0,a,"/"); print a[5]}' /tmp/go.mod` && make build
# get only the plan's go.mod and get the plan's name from it

#RUN cd /go-spacemesh/plans/`awk '/^module/ {split($0,a,"/"); print a[5]}' /tmp/go.mod` && go build -a -o /testplan

## The 'AS runtime' token is used to parse Docker stdout to extract the build image ID to cache.
FROM ${run_image} AS runtime


## TODO: should we just start from another base image that is not busybox? does testground require busybox?
#COPY --from=builder /lib/x86_64-linux-gnu/librt-2.28.so /lib/librt-2.28.so
#COPY --from=builder /lib/x86_64-linux-gnu/librt.so.1 /lib/librt.so.1
#COPY --from=builder /lib/x86_64-linux-gnu/libdl-2.28.so /lib/libdl-2.28.so
#COPY --from=builder /lib/x86_64-linux-gnu/libdl.so.2 /lib/libdl.so.2
COPY --from=builder /build/testplan /testplan
COPY --from=builder /build/libgpu-setup.so /libgpu-setup.so

EXPOSE 6060
ENTRYPOINT [ "/testplan"]
