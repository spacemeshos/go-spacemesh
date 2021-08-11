# This Dockerfile performs a multi-stage build and RUNTIME_IMAGE is the image
# onto which to copy the resulting binary.
#
# Picking a different runtime base image from the build image allows us to
# slim down the deployable considerably.
#
# The user can override the runtime image by passing in the appropriate builder
# configuration option.
ARG run_image=busybox:1.31.1-glibc
ARG build_image=golang:1.16-buster
#:::
#::: BUILD CONTAINER
#:::
FROM ${build_image} AS builder

# COPY extra/go-spacemesh/[a-z]* /go-spacemesh/
COPY extra/go-spacemesh /go-spacemesh
# get only the plan's go.mod and get the plan's name from it
COPY plan/go.mod /tmp/go.mod
RUN cd /go-spacemesh/plans/`awk '/^module/ {split($0,a,"/"); print a[5]}' /tmp/go.mod` && go build -a -o /testplan

## The 'AS runtime' token is used to parse Docker stdout to extract the build image ID to cache.
FROM ${run_image} AS runtime

COPY --from=builder /testplan /testplan

EXPOSE 6060
ENTRYPOINT [ "/testplan"]
