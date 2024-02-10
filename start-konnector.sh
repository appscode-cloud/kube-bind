#!/bin/bash
set -ex

cd /home/rasel/go/src/go.kubeware.dev/kubeware/
go install -v ./...
#./bin/konnector
IGNORE_GO_VERSION=1 make build
docker build --tag superm4n/konnector .
docker push superm4n/konnector
kubectl delete pod -n kubeware --field-selector=metadata.namespace==kubeware