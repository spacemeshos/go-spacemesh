version_info ?= $(shell git rev-parse --short HEAD)
date := $(shell date +'%s')

test_name ?= TestSmeshing
org ?= spacemeshos
image_name ?= $(org)/systest:$(version_info)
poet_image ?= spacemeshos/poet:87608eda8307b44984c191afc65cdbcec0d8d1c4
smesher_image ?= $(org)/go-spacemesh-dev:$(version_info)
test_id ?= systest-$(version_info)
test_pod_name ?= systest-$(version_info)-$(date)
keep ?= false
clusters ?= 1
size ?= 10
poet_size ?= 3
level ?= debug
bootstrap ?= 5m
storage ?= standard=1Gi
node_selector ?=
namespace ?=
label ?=
env ?=

configname ?= $(test_pod_name)
smesher_config ?= parameters/fastnet/smesher.json
poet_config ?= parameters/fastnet/poet.conf
properties_file ?=
ifneq ($(properties_file),)
	optional_properties = --from-env-file=$(properties_file)
endif 
ifeq ($(configname),$(test_pod_name))
	run_deps = config
endif


.PHONY: docker
docker:
	@DOCKER_BUILDKIT=1 docker build ../ -f Dockerfile -t $(image_name)

.PHONY: push
push:
	docker push $(image_name)

.PHONY: config
config:
	@kubectl create configmap $(configname) \
		--from-file=smesher=$(smesher_config) --from-file=poet=$(poet_config) \
		--from-literal=cluster-size=$(size) \
		--from-literal=bootsrap-duration=$(bootstrap) \
		$(optional_properties)
	@kubectl label configmap $(configname) testid=$(test_id)

.PHONY: run
run: $(run_deps)
	@echo "launching test pod with name=$(test_pod_name) and testid=$(test_id)"
	@kubectl run --image $(image_name) $(test_pod_name) \
	--env=$(env) \
	--restart=Never \
	--labels="testid=$(test_id)" \
    --image-pull-policy=IfNotPresent -- \
    tests -test.v -test.timeout=0 -test.run=$(test_name) -namespace=$(namespace) -clusters=$(clusters) -storage=$(storage) -poet-size=$(poet_size) -image=$(smesher_image) -poet-image=$(poet_image) -level=$(level) -node-selector=$(node_selector) -keep=$(keep) -testid=$(test_id) -labels=$(label) -configname=$(configname)
	-@kubectl wait --timeout=10s --for=condition=ready pod/$(test_pod_name)
	@kubectl logs $(test_pod_name) -f

.PHONY: clean
clean:
	@echo "deleting configmaps with testid=$(test_id)"
	@kubectl delete configmap --selector=testid=$(test_id)
	@echo "deleting test pods with testid=$(test_id)"
	@kubectl delete pod --selector=testid=$(test_id)
	@kubectl wait --for delete pod --selector=testid=$(test_id)
	@echo "deleting ephemeral namespaces with testid=$(test_id)"
	@kubectl delete ns --selector=testid=$(test_id),keep=false
	@kubectl wait --timeout=60s --for delete namespace --selector=testid=$(test_id),keep=false

.PHONY: cleanall
cleanall: clean
	@echo "deleting all namespaces with testid=$(test_id)"
	@kubectl delete ns --selector=testid=$(test_id)
	@kubectl wait --timeout=60s --for delete namespace --selector=testid=$(test_id)
