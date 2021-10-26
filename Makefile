TAG ?= $(shell date '+%Y%m%d%H%M')

build:
	CGO_ENABLED=0 go build -o bin/sigbench microsoft.com/sigbench/cmd

agent: build
	bin/sigbench --master http://127.0.0.1:8000 -l 127.0.0.1:7000

master: build
	bin/sigbench -l :8000 -mode "service"

container-build:
	docker build -t ${REGISTRY}/sigbench:${TAG} .

container-push:
	docker push ${REGISTRY}/sigbench:${TAG}
