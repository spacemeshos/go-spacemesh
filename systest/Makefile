test_name ?= TestSmeshing
cron_name ?= 
version_info ?= $(shell git rev-parse --short HEAD)
org ?= spacemeshos
image_name ?= $(org)/systest:$(version_info)
smesher_image ?= $(org)/go-spacemesh-dev:$(version_info)
test_pod_name ?= systest-$(version_info)
keep ?= false
clusters ?= 1
size ?= 10
level ?= debug
bootstrap ?= 5m
node_selector ?=
namespace ?=
schedule ?=

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
	tests -test.v -test.timeout=0 -test.run=$(test_name) -namespace=$(namespace) -clusters=$(clusters) -size=$(size) -image=$(smesher_image) -level=$(level) -node-selector=$(node_selector) -bootstrap=$(bootstrap) -keep=$(keep) -testid=$(test_pod_name)

.PHONY: watch
watch:
	@kubectl wait --for=condition=ready pod/$(test_pod_name)
	@kubectl logs $(test_pod_name) -f

.PHONY: clean
clean:
	@kubectl delete pod/$(test_pod_name)
	@kubectl delete ns --selector=testid=$(test_pod_name),keep=false

.PHONY: cleanlong
cleanlong:
	@kubectl delete pod/$(test_pod_name)
	@kubectl delete ns --selector=testid=$(test_pod_name)
	@kubectl delete cronjob --selector=testid=$(test_pod_name)

.PHONY: cron
cron:
	@test_name=$(test_name) namespace=$(namespace) size=$(size) smesher_image=$(smesher_image) level=$(level) node_selector=$(node_selector) keep=true testid=$(test_pod_name) cron_name=$(cron_name) image_name=$(image_name) schedule=$(schedule) envsubst < ./templates/cronjob.yaml | kubectl create -f -