version_info ?= $(shell git rev-parse --short HEAD)
date := $(shell date +'%s')
tmpfile := $(shell mktemp /tmp/systest-XXX)

test_name ?= TestSmeshing
org ?= spacemeshos
image_name ?= $(org)/systest:$(version_info)
poet_image ?= spacemeshos/poet:v0.8.5
smesher_image ?= $(org)/go-spacemesh-dev:$(version_info)
bs_image ?= $(org)/go-spacemesh-dev-bs:$(version_info)
test_id ?= systest-$(version_info)
test_job_name ?= systest-$(version_info)-$(date)
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
count ?= 1

configname ?= $(test_job_name)
smesher_config ?= parameters/fastnet/smesher.json
poet_config ?= parameters/fastnet/poet.conf
properties ?=
ifneq ($(properties),)
	optional_properties = --from-env-file=$(properties)
endif
ifeq ($(configname),$(test_job_name))
	run_deps = config
endif

command := tests -test.v -test.count=$(count) -test.timeout=0 -test.run=$(test_name) -clusters=$(clusters) \
-level=$(level) -labels=$(label) -configname=$(configname)

.PHONY: docker
docker:
	@DOCKER_BUILDKIT=1 docker build ../ -f Dockerfile -t $(image_name)

.PHONY: push
push:
	docker push $(image_name)

.PHONY: template
template:
	@echo smesher=`cat $(smesher_config)` > $(tmpfile)
	@echo poet=`cat $(poet_config)` >> $(tmpfile)
	@echo cluster-size=$(size) >> $(tmpfile)
	@echo bootstrap-duration=$(bootstrap) >> $(tmpfile)
	@echo namespace=$(namespace) >> $(tmpfile)
	@echo storage=$(storage) >> $(tmpfile)
	@echo node-selector=$(node_selector) >> $(tmpfile)
	@echo image=$(smesher_image) >> $(tmpfile)
	@echo bs-image=$(bs_image) >> $(tmpfile)
	@echo poet-image=$(poet_image) >> $(tmpfile)
	@echo poet-size=$(poet_size) >> $(tmpfile)
	@echo keep=$(keep) >> $(tmpfile)
	@echo testid=$(test_id) >> $(tmpfile)

.PHONY: config
config: template
	@kubectl create configmap $(configname) --from-env-file=$(tmpfile) $(optional_properties)
	@kubectl label configmap $(configname) testid=$(test_id)
	@rm $(tmpfile)

.PHONY: gomplate
gomplate:
	@go install github.com/hairyhenderson/gomplate/v3/cmd/gomplate@latest

.PHONY: run
run: gomplate $(run_deps)
	@echo "launching test job with name=$(test_job_name) and testid=$(test_id)"
	@testid=$(test_id) job_name=$(test_job_name) image=$(image_name) command="$(command)" gomplate --file systest_job.yml.tmpl | kubectl apply -f -
	-@kubectl wait --timeout=20s --for=condition=ready -l job-name=$(test_job_name) pod
	kubectl logs job/$(test_job_name) -f --ignore-errors
	test_job_name=$(test_job_name) ./wait_for_job.sh

.PHONY: clean
clean:
	@echo "deleting configmaps with testid=$(test_id)"
	@kubectl delete configmap --selector=testid=$(test_id)
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
