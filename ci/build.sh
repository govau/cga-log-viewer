#!/bin/bash

set -eu
set -o pipefail

# Tag is not always populated correctly by the docker-image resource (ie it defaults to latest)
# so use the actual source for tag
TAG=$(cat src/.git/ref)
REPO=$(cat img/repository)
LOG_PROXY_TAG=$(cat log-proxy-src/.git/ref)
LOG_PROXY_REPO=$(cat log-proxy-img/repository)
ES_PROXY_TAG=$(cat es-proxy-src/.git/ref)
ES_PROXY_REPO=$(cat es-proxy-img/repository)

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
apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "${ENV}cld-log-viewer"
spec:
  targets:
  - name: "${ENV}cld-log-viewer" # TODO - remove this softness that allows HTTP traffic to this service - currently here so that the ingress controller is happy
  peers:
  - mtls: {}
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
        resources: {limits: {memory: "64Mi", cpu: "250m"}}
        env:
        - name: PORT
          value: "5601"
        - name: ES_END_POINT
          value: http://localhost:9300
        envFrom:
        - secretRef: {name: ${ENV}cld-log-viewer}
        - secretRef: {name: shared-log-viewer}
      - name: ${ENV}cld-log-proxy
        image: ${LOG_PROXY_REPO}:${LOG_PROXY_TAG}
        ports:
        - name: http
          containerPort: 9300
        resources: {limits: {memory: "64Mi", cpu: "250m"}}
        args:
        - -listen
        - :9300
        - -endpoint
        - http://localhost:9200
        - -filter
        - '{"term":{"@cf.env.keyword":"${ENV}.cld.gov.au"}}'
        envFrom:
        - secretRef: {name: aws-es-proxy}
      - name: ${ENV}cld-es-proxy
        image: ${ES_PROXY_REPO}:${ES_PROXY_TAG}
        ports:
        - name: http
          containerPort: 9200
        resources: {limits: {memory: "64Mi", cpu: "750m"}}
        args:
        - -listen
        - :9200
        - -endpoint
        - "\$(AWS_ES_URL)"
        envFrom:
        - secretRef: {name: aws-es-proxy}


---
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name:  ${ENV}cld-log-viewer
  annotations:
    kubernetes.io/tls-acme: "true"
    certmanager.k8s.io/cluster-issuer: "letsencrypt-prod"
    ingress.kubernetes.io/force-ssl-redirect: "true"
spec:
  tls:
  - secretName: ${ENV}cld-logs-certificate
    hosts:
    - ${ENV}cld-logs.kapps.l.cld.gov.au
  rules:
  - host: ${ENV}cld-logs.kapps.l.cld.gov.au
    http:
      paths:
      - backend:
          serviceName: ${ENV}cld-log-viewer
          servicePort: 5601
EOF

cat deployment.yaml

echo $KUBECONFIG > k
export KUBECONFIG=k

kubectl apply --record -f - < deployment.yaml
kubectl rollout status deployment.apps/${ENV}cld-log-viewer
