#!/usr/bin/env bash
# Deploy Harbor into k3d for e2e testing.
# Linux amd64 only — Harbor images are amd64 and crash under QEMU on ARM.
#
# Usage:
#   ./harbor-setup.sh          # install (creates k3d cluster + deploys Harbor)
#   ./harbor-setup.sh teardown # stop and clean
set -euo pipefail

CLUSTER_NAME="oci-source-e2e"
HARBOR_PORT="${HARBOR_PORT:-30080}"
HARBOR_PASSWORD="Harbor12345"
HARBOR_NAMESPACE="harbor"

if [[ "$(uname -s)" != "Linux" ]] || [[ "$(uname -m)" != "x86_64" ]]; then
    echo "ERROR: Harbor e2e requires Linux x86_64. Current: $(uname -s)/$(uname -m)"
    exit 1
fi

teardown() {
    echo "Tearing down k3d cluster..."
    k3d cluster delete "$CLUSTER_NAME" 2>/dev/null || true
}

if [[ "${1:-}" == "teardown" ]]; then
    teardown
    exit 0
fi

for cmd in k3d kubectl helm docker crane; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: $cmd is required"
        exit 1
    fi
done

# Create k3d cluster (or reuse existing)
if k3d cluster list 2>/dev/null | grep -q "$CLUSTER_NAME"; then
    echo "=== Reusing existing k3d cluster $CLUSTER_NAME ==="
    k3d kubeconfig merge "$CLUSTER_NAME" --kubeconfig-switch-context
else
    echo "=== Creating k3d cluster ==="
    k3d cluster create "$CLUSTER_NAME" \
        -p "${HARBOR_PORT}:30080@server:0" \
        -p "30004:30004@server:0" \
        --k3s-arg "--disable=traefik@server:0" \
        --wait

    echo "Waiting for nodes..."
    kubectl wait --for=condition=Ready nodes --all --timeout=120s
fi

echo "=== Deploying Docker Registry (for generic OCI tests) ==="
kubectl apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: docker-registry
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: docker-registry
  template:
    metadata:
      labels:
        app: docker-registry
    spec:
      containers:
      - name: registry
        image: registry:2
        ports:
        - containerPort: 5000
        env:
        - name: REGISTRY_STORAGE_DELETE_ENABLED
          value: "true"
---
apiVersion: v1
kind: Service
metadata:
  name: docker-registry
  namespace: default
spec:
  type: NodePort
  selector:
    app: docker-registry
  ports:
  - port: 5000
    targetPort: 5000
    nodePort: 30004
EOF
kubectl rollout status deployment/docker-registry --timeout=120s

echo "=== Deploying Harbor ==="
kubectl create namespace "$HARBOR_NAMESPACE" 2>/dev/null || true
helm repo add harbor https://helm.goharbor.io 2>/dev/null || true
helm repo update harbor

helm upgrade --install harbor harbor/harbor \
    --namespace "$HARBOR_NAMESPACE" \
    --set expose.type=nodePort \
    --set "expose.nodePort.ports.http.nodePort=${HARBOR_PORT}" \
    --set expose.tls.enabled=false \
    --set externalURL="http://localhost:${HARBOR_PORT}" \
    --set harborAdminPassword="${HARBOR_PASSWORD}" \
    --set persistence.enabled=false \
    --set trivy.enabled=false \
    --wait --timeout=600s

echo "=== Waiting for Harbor to be healthy ==="
for i in $(seq 1 60); do
    if curl -sf "http://localhost:${HARBOR_PORT}/api/v2.0/health" >/dev/null 2>&1; then
        echo ""
        echo "=== All services ready ==="
        echo "Docker Registry: http://localhost:30004"
        echo "Harbor:          http://localhost:${HARBOR_PORT} (admin/${HARBOR_PASSWORD})"
        exit 0
    fi
    printf "."
    sleep 5
done

echo ""
echo "ERROR: Harbor did not become healthy"
kubectl -n "$HARBOR_NAMESPACE" get pods
kubectl -n "$HARBOR_NAMESPACE" logs deployment/harbor-core --tail=30
exit 1
