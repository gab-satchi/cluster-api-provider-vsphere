#!/bin/bash
# Deploy CAPV to the given cluster
#
# Usage:
# $ deploy-local.sh <dependencies-yaml> <infrastructure-yaml>

set -o errexit
set -o pipefail
set -o nounset

DEPENDENCIES=$1
INFRASTRUCTURE=$2
KUBECTL="$(command -v kubectl)"

CERTMANAGER_NAMESPACE="vmware-system-cert-manager"
CERTMANAGER_DEPLOYMENTS=(
  cert-manager
  cert-manager-cainjector
  cert-manager-webhook
)

CAPV_NAMESPACE="capv-system"
CAPV_SYSTEM_NAMESPACE="vmware-system-capv"

CAPV_DEPLOYMENT="capv-controller-manager"
CAPI_DEPLOYMENT="capi-controller-manager"
CABPK_DEPLOYMENT="capi-kubeadm-bootstrap-controller-manager"
KCP_DEPLOYMENT="capi-kubeadm-control-plane-controller-manager"

NODE_COUNT=$(kubectl get node --no-headers 2>/dev/null | wc -l)
if [ "$NODE_COUNT" -eq 1 ]; then
  sed -i -e 's/replicas: 3/replicas: 1/g' "$DEPENDENCIES"
  # remove the generated '-e' file on Mac
  rm -f "$DEPENDENCIES-e"
fi

DEP_EXISTS=""
if $KUBECTL get deployment -n "${CERTMANAGER_NAMESPACE}" "${CERTMANAGER_DEPLOYMENTS[0]}" >/dev/null 2>&1 ; then
  DEP_EXISTS="exists"
fi

# Deploy the dependencies first then wait on cert-manager
$KUBECTL apply -f "$DEPENDENCIES"
if [[ -z $DEP_EXISTS ]]; then
  for dep in "${CERTMANAGER_DEPLOYMENTS[@]}"; do
    $KUBECTL rollout status -n "${CERTMANAGER_NAMESPACE}" deployment "${dep}"
  done

  # Find a better way to wait for this...
  echo $'\nSleeping for 60s - waiting for webhook to be initialized\n'
  sleep 60
fi

# Deploy the infrastructure
if [ "$NODE_COUNT" -eq 1 ]; then
  sed -i -e 's/replicas: 3/replicas: 1/g' "$INFRASTRUCTURE"
  # remove the generated '-e' file on Mac
  rm -f "$INFRASTRUCTURE-e"
fi

INFRA_EXISTS=""
if $KUBECTL get deployment -n "${CAPV_NAMESPACE}" "${CAPV_DEPLOYMENT}" >/dev/null 2>&1 ; then
    INFRA_EXISTS="exists"
fi

$KUBECTL apply -f "$INFRASTRUCTURE"
if [[ -z $INFRA_EXISTS ]]; then
    $KUBECTL rollout restart -n "${CAPV_NAMESPACE}" deployment "${CAPV_DEPLOYMENT}"
    $KUBECTL rollout restart -n "${CAPV_SYSTEM_NAMESPACE}" deployment "${CAPI_DEPLOYMENT}"
    $KUBECTL rollout restart -n "${CAPV_SYSTEM_NAMESPACE}" deployment "${CABPK_DEPLOYMENT}"
    $KUBECTL rollout restart -n "${CAPV_SYSTEM_NAMESPACE}" deployment "${KCP_DEPLOYMENT}"

    $KUBECTL rollout status -n "${CAPV_SYSTEM_NAMESPACE}" deployment "${KCP_DEPLOYMENT}"
    $KUBECTL rollout status -n "${CAPV_SYSTEM_NAMESPACE}" deployment "${CABPK_DEPLOYMENT}"
    $KUBECTL rollout status -n "${CAPV_SYSTEM_NAMESPACE}" deployment "${CAPI_DEPLOYMENT}"
    $KUBECTL rollout status -n "${CAPV_NAMESPACE}" deployment "${CAPV_DEPLOYMENT}"
fi
