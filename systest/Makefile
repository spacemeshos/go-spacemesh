test_name ?= TestSmeshing
version_info ?= $(shell git rev-parse --short HEAD)
org ?= spacemeshos
image_name ?= $(org)/systest:$(version_info)
poet_image ?= spacemeshos/poet:87608eda8307b44984c191afc65cdbcec0d8d1c4
smesher_image ?= $(org)/go-spacemesh-dev:$(version_info)
test_id ?= systest-$(version_info)
test_job_name ?= systest-$(version_info)-$(shell date +'%s')
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

command := tests -test.v -test.timeout=0 -test.run=$(test_name) -namespace=$(namespace) \
	-clusters=$(clusters) -storage=$(storage) -size=$(size) -poet-size=$(poet_size) \
	-image=$(smesher_image) -poet-image=$(poet_image) -level=$(level) -node-selector=$(node_selector) \
	-bootstrap=$(bootstrap) -keep=$(keep) -testid=$(test_id) -labels=$(label)

.PHONY: docker
docker:
	@DOCKER_BUILDKIT=1 docker build ../ -f Dockerfile -t $(image_name)

.PHONY: push
push:
	docker push $(image_name)

.PHONY: gomplate
gomplate:
	@go install github.com/hairyhenderson/gomplate/v3/cmd/gomplate@latest

.PHONY: run
run: gomplate
	@echo "launching test job with name=$(test_job_name) and testid=$(test_id)"
	@testid=$(test_id) job_name=$(test_job_name) image=$(image_name) command="$(command)" gomplate --file systest_job.yml.tmpl | kubectl apply -f -
	-@kubectl wait --timeout=20s --for=condition=ready -l job-name=$(test_job_name) pod
	kubectl logs job/$(test_job_name) -f --ignore-errors
	test_job_name=$(test_job_name) ./wait_for_job.sh

.PHONY: clean
clean:
	@echo "deleting test pods with testid=$(test_id)"
	@kubectl delete job --selector=testid=$(test_id)
	@kubectl wait --for delete job --selector=testid=$(test_id)
	@echo "deleting ephemeral namespaces with testid=$(test_id)"
	@kubectl delete ns --selector=testid=$(test_id),keep=false
	@kubectl wait --timeout=60s --for delete namespace --selector=testid=$(test_id),keep=false

.PHONY: cleanall
cleanall: clean
	@echo "deleting all namespaces with testid=$(test_id)"
	@kubectl delete ns --selector=testid=$(test_id)
	@kubectl wait --timeout=60s --for delete namespace --selector=testid=$(test_id)
