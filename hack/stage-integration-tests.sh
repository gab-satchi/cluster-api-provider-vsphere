#!/bin/bash

set -o errexit
set -o pipefail
set -o nounset
set -x

coverage_file="${1-}"

# Update PATH so binaries installed in the workspace are available.
export PATH=$PWD:$PATH

# Ensure that the correct YAML is applied to the cluster
make deploy-local-with-vmop

# Convert "go test" output into JUnit XML report (so that Jenkins can display it properly)
./hack/test-integration.sh "${coverage_file}" 2>&1 | tee integration-tests.out | go-junit-report > integration-tests-report.xml
