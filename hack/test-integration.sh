#!/bin/bash

# Run the integration tests.
# If an argument is given, the coverage will be recorded:
# test-integration.sh [<coverage file>]

set -o errexit
set -o nounset
set -o pipefail

function join_packages { local IFS=","; echo "$*"; }

# Change directories to the parent directory of the one in which this
# script is located.
cd "$(dirname "${BASH_SOURCE[0]}")/.."

COVERAGE_FILE="${1-}"
INTEGRATION_COVERAGE_FILE="$(pwd)/int.cover.out"
ENVIRONMENT_COVERAGE_FILE="$(pwd)/env.cover.out"

# shellcheck disable=SC1091
source hack/ensure-go.sh

# Initialize GOFLAGS by indicating verbose output.
GOFLAGS="-v"

# The "-count=1" argument is the idiomatic way to ensure all tests
# are run and the cached test results are ignored. Please see
# the output of "go help test" for more information.
GOFLAGS="${GOFLAGS} -count=1"

packages=(
  "./controllers/..."
  "./pkg/..."
  "./apis/..."
)
COV_OPTS=$(join_packages "${packages[@]}")

# The first argument is the name of the coverage file to use.
if [ -n "${COVERAGE_FILE}" ]; then
    ENV_GOFLAGS="${GOFLAGS} -coverprofile=${ENVIRONMENT_COVERAGE_FILE} -coverpkg=${COV_OPTS}"
    INT_GOFLAGS="${GOFLAGS} -tags=integration -coverprofile=${INTEGRATION_COVERAGE_FILE} -coverpkg=${COV_OPTS}"
fi

# Run the integration tests.
# GOFLAGS="${GOFLAGS:-${INT_GOFLAGS-}}" go test ./apis/... ./controllers/... ./pkg/... ./controllers-test/...

# Run the Kind tests.
GOFLAGS="${GOFLAGS:-${ENV_GOFLAGS-}}" go test ./test/integration/...

# Merge the two coverage files.
if [ -n "${COVERAGE_FILE}" ]; then
    touch "${ENVIRONMENT_COVERAGE_FILE}" "${INTEGRATION_COVERAGE_FILE}"
    hack/tools/bin/gocovmerge "${ENVIRONMENT_COVERAGE_FILE}" "${INTEGRATION_COVERAGE_FILE}" >"${COVERAGE_FILE}"
    rm -f "${ENVIRONMENT_COVERAGE_FILE}" "${INTEGRATION_COVERAGE_FILE}"
fi
