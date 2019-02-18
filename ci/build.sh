#!/bin/bash

set -eu
set -o pipefail

# Tag is not always populated correctly by the docker-image resource (ie it defaults to latest)
# so use the actual source for tag
TAG=$(cat src/.git/ref)
REPO=$(cat img/repository)

cat <<EOF > deployment.yaml
kind: Service
apiVersion: v1
metadata:
  name: ${ENV}cld-log-viewer
spec:
  selector:
    app: ${ENV}cld-log-viewer
  ports:
  - protocol: TCP
    port: 5601
    targetPort: 5601
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: ${ENV}cld-cf
spec:
  hosts:
  - "api.system.${ENV}.cld.gov.au"
  ports:
  - number: 443
    name: https
    protocol: HTTPS
  resolution: DNS
  location: MESH_EXTERNAL
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: ${ENV}cld-cf
spec:
  hosts:
  - "api.system.${ENV}.cld.gov.au"
  tls:
  - match:
    - port: 443
      sni_hosts:
      - "api.system.${ENV}.cld.gov.au"
    route:
    - destination:
        host: "api.system.${ENV}.cld.gov.au"
        port:
          number: 443
      weight: 100
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: ${ENV}cld-uaa
spec:
  hosts:
  - "uaa.system.${ENV}.cld.gov.au"
  ports:
  - number: 443
    name: https
    protocol: HTTPS
  resolution: DNS
  location: MESH_EXTERNAL
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: ${ENV}cld-uaa
spec:
  hosts:
  - "uaa.system.${ENV}.cld.gov.au"
  tls:
  - match:
    - port: 443
      sni_hosts:
      - "uaa.system.${ENV}.cld.gov.au"
    route:
    - destination:
        host: "uaa.system.${ENV}.cld.gov.au"
        port:
          number: 443
      weight: 100
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${ENV}cld-log-viewer
  labels:
    app: ${ENV}cld-log-viewer
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${ENV}cld-log-viewer
  template:
    metadata:
      labels:
        app: ${ENV}cld-log-viewer
    spec:
      containers:
      - name: ${ENV}cld-log-viewer
        image: ${REPO}:${TAG}
        ports:
        - name: http
          containerPort: 5601
        resources: {limits: {memory: "64Mi", cpu: "100m"}}
        env:
        - name: PORT
          value: "5601"
        - name: INSECURE
          value: "true" # used for cookies, we need to enable TLS serving before re-instating
        envFrom:
        - secretRef: {name: ${ENV}cld-log-viewer}
        - secretRef: {name: shared-log-viewer}
EOF

cat deployment.yaml

echo $KUBECONFIG > k
export KUBECONFIG=k

kubectl apply --record -f - < deployment.yaml
kubectl rollout status deployment.apps/${ENV}cld-log-viewer
