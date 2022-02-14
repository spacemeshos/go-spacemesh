test_name ?= TestSmeshing
version_info ?= $(shell git rev-parse --short HEAD)
image_name ?= yashulyak/systest:$(version_info)
smesher_image ?= spacemeshos/go-spacemesh-dev:store-ordered
test_pod_name ?= systest-$(version_info)
clusters ?= 1
size ?= 10

.PHONY: docker
docker:
	@DOCKER_BUILDKIT=1 docker build . -t $(image_name)

.PHONY: push
push:
	docker push $(image_name)

.PHONY: run
run: launch watch

.PHONY: launch
launch:
	@kubectl run --image $(image_name) $(test_pod_name) \
	--restart=Never \
	--image-pull-policy=IfNotPresent -- \
	tests -test.v -test.timeout=0 -test.run=$(test_name) -clusters=$(clusters) -size=$(size) -image=$(smesher_image) -level=debug -node-selector=cloud.google.com/gke-nodepool=dmitry-ci-preemtible-1 -bootstrap=10m

.PHONY: watch
watch:
	@kubectl wait --for=condition=ready pod/$(test_pod_name)
	@kubectl logs $(test_pod_name) -f

.PHONY: clean
clean:
	@kubectl delete pod/$(test_pod_name)