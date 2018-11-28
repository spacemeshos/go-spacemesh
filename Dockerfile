FROM partlab/ubuntu-golang AS build
ARG BRANCH=develop
RUN apt-get update
RUN apt-get -y install git build-essential rsyslog

RUN service rsyslog start
#RUN go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
#RUN go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
#RUN go get -u github.com/golang/protobuf/protoc-gen-go
RUN go get -u github.com/kardianos/govendor
RUN echo ${BRANCH}
RUN mkdir -p src/github.com/spacemeshos; cd src/github.com/spacemeshos; mkdir -p go-spacemesh; cd go-spacemesh;
ADD . src/github.com/spacemeshos/go-spacemesh
RUN cd src/github.com/spacemeshos/go-spacemesh; go build; govendor sync; make
RUN cp /go/src/github.com/spacemeshos/go-spacemesh/config.toml /go

ENTRYPOINT /go/src/github.com/spacemeshos/go-spacemesh/go-spacemesh
EXPOSE 7513
