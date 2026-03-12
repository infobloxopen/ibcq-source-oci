#!/usr/bin/env bash
# Setup k3d cluster with Harbor and Docker Registry for e2e testing.
# Usage: ./setup.sh [teardown]
set -euo pipefail

CLUSTER_NAME="oci-source-e2e"
HARBOR_PORT=30003
REGISTRY_PORT=30004
HARBOR_NAMESPACE="harbor"

teardown() {
    echo "Tearing down k3d cluster..."
    k3d cluster delete "$CLUSTER_NAME" 2>/dev/null || true
}

if [[ "${1:-}" == "teardown" ]]; then
    teardown
    exit 0
fi

# Check prerequisites
for cmd in k3d kubectl helm docker; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: $cmd is required but not installed."
        exit 1
    fi
done

# Teardown any existing cluster
teardown

echo "=== Creating k3d cluster ==="
k3d cluster create "$CLUSTER_NAME" \
    -p "${HARBOR_PORT}:30003@server:0" \
    -p "${REGISTRY_PORT}:30004@server:0" \
    --wait

echo "=== Waiting for cluster to be ready ==="
kubectl wait --for=condition=Ready nodes --all --timeout=120s

echo "=== Deploying plain Docker Registry ==="
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
        - name: REGISTRY_HTTP_ADDR
          value: ":5000"
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

echo "=== Deploying Harbor ==="
kubectl create namespace "$HARBOR_NAMESPACE" 2>/dev/null || true

helm repo add harbor https://helm.goharbor.io 2>/dev/null || true
helm repo update

# Install Harbor with NodePort and minimal config for testing
helm upgrade --install harbor harbor/harbor \
    --namespace "$HARBOR_NAMESPACE" \
    --set expose.type=nodePort \
    --set expose.nodePort.ports.https.nodePort=30003 \
    --set expose.tls.auto.commonName=localhost \
    --set externalURL=https://localhost:30003 \
    --set harborAdminPassword=Harbor12345 \
    --set persistence.enabled=false \
    --set trivy.enabled=false \
    --set notary.enabled=false \
    --wait --timeout=300s

echo "=== Waiting for Harbor to be ready ==="
kubectl -n "$HARBOR_NAMESPACE" rollout status deployment/harbor-core --timeout=300s
kubectl -n "$HARBOR_NAMESPACE" rollout status deployment/harbor-registry --timeout=300s

echo "=== Waiting for Docker Registry to be ready ==="
kubectl rollout status deployment/docker-registry --timeout=120s

echo ""
echo "=== Setup Complete ==="
echo "Harbor:          https://localhost:${HARBOR_PORT} (admin/Harbor12345)"
echo "Docker Registry: http://localhost:${REGISTRY_PORT}"
echo ""
echo "To push test images:"
echo "  docker tag alpine localhost:${REGISTRY_PORT}/test/alpine:latest"
echo "  docker push localhost:${REGISTRY_PORT}/test/alpine:latest"
echo ""
echo "To teardown: $0 teardown"
