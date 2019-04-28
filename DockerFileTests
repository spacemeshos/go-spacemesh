FROM python:latest
ARG GCLOUD_KEY
ARG PROJECT_NAME
ARG CLUSTER_NAME
ARG CLUSTER_ZONE

COPY . .

ADD  . /go-spacemesh

WORKDIR /go-spacemesh/tests

RUN pip install pytest
RUN pip install -r requirements.txt

RUN curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl
RUN chmod +x ./kubectl
RUN mv ./kubectl /usr/local/bin

# Downloading gcloud package
RUN curl https://dl.google.com/dl/cloudsdk/release/google-cloud-sdk.tar.gz > /tmp/google-cloud-sdk.tar.gz

# Installing the package
RUN mkdir -p /usr/local/gcloud \
  && tar -C /usr/local/gcloud -xvf /tmp/google-cloud-sdk.tar.gz \
  && /usr/local/gcloud/google-cloud-sdk/install.sh

# Adding the package path to local
ENV PATH $PATH:/usr/local/gcloud/google-cloud-sdk/bin

ENV GCLOUD_KEY ${GCLOUD_KEY}
ENV PROJECT_NAME ${PROJECT_NAME}
ENV CLUSTER_NAME ${CLUSTER_NAME}
ENV CLUSTER_ZONE ${CLUSTER_ZONE}
RUN chmod +x ./k8s/gcp_connect.sh
RUN sh ./k8s/gcp_connect.sh
