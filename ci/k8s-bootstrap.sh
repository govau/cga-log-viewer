#!/bin/bash

set -eu
set -o pipefail

: ${ES_HOSTNAME?ES_HOSTNAME must be set}
NAMESPACE=${NAMESPACE:-cf-log-viewer}

ci_user="ci-user"

kubectl apply -f <(cat <<EOF
# Create our own namespace
apiVersion: v1
kind: Namespace
metadata:
  name: "${NAMESPACE}"
EOF
)

kubectl -n "${NAMESPACE}" apply -f <(cat <<EOF
# Create a service account for deployment
apiVersion: v1
kind: ServiceAccount
metadata:
  name: "${ci_user}"
---
kind: Role
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: istio-config
rules:
- apiGroups: ["networking.istio.io"]
  resources: ["serviceentry", "virtualservice"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ci-user-can-do-istio-config
roleRef:
  name: istio-config
  kind: Role
  apiGroup: rbac.authorization.k8s.io
subjects:
- name: "${ci_user}"
  namespace: "${NAMESPACE}"
  kind: ServiceAccount
---
# Give appropriate permissions for being able to
# deploy into this namespace
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ci-user-can-deploy
roleRef:
  name: edit
  kind: ClusterRole
  apiGroup: rbac.authorization.k8s.io
subjects:
- name: "${ci_user}"
  namespace: "${NAMESPACE}"
  kind: ServiceAccount
---
apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "default"
spec:
  peers:
  - mtls:
      mode: STRICT
---
apiVersion: "networking.istio.io/v1alpha3"
kind: "DestinationRule"
metadata:
  name: "default"
spec:
  host: "*.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: es
spec:
  hosts:
  - "${ES_HOSTNAME}"
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
  name: es
spec:
  hosts:
  - "${ES_HOSTNAME}"
  tls:
  - match:
    - port: 443
      sni_hosts:
      - "${ES_HOSTNAME}"
    route:
    - destination:
        host: "${ES_HOSTNAME}"
        port:
          number: 443
      weight: 100
EOF
)

secret="$(kubectl get "serviceaccount/${ci_user}" --namespace "${NAMESPACE}" -o=jsonpath='{.secrets[0].name}')"
token="$(kubectl get secret "${secret}" --namespace "${NAMESPACE}" -o=jsonpath='{.data.token}' | base64 -d)"

cur_context="$(kubectl config view -o=jsonpath='{.current-context}' --flatten=true)"
cur_cluster="$(kubectl config view -o=jsonpath="{.contexts[?(@.name==\"${cur_context}\")].context.cluster}" --flatten=true)"
cur_api_server="$(kubectl config view -o=jsonpath="{.clusters[?(@.name==\"${cur_cluster}\")].cluster.server}" --flatten=true)"
cur_crt="$(kubectl config view -o=jsonpath="{.clusters[?(@.name==\"${cur_cluster}\")].cluster.certificate-authority-data}" --flatten=true)"

kubeconfig="$(cat <<EOF
{
  "apiVersion": "v1",
  "clusters": [
    {
      "cluster": {
        "certificate-authority-data": "${cur_crt}",
        "server": "${cur_api_server}"
      },
      "name": "kubernetes"
    }
  ],
  "contexts": [
    {
      "context": {
        "cluster": "kubernetes",
        "user": "${ci_user}",
        "namespace": "${NAMESPACE}"
      },
      "name": "kubernetes"
    }
  ],
  "current-context": "kubernetes",
  "kind": "Config",
  "users": [
    {
      "name": "${ci_user}",
      "user": {
        "token": "${token}"
      }
    }
  ]
}
EOF
)"

echo "You may wish to run the following..."
echo 'credhub set -n /concourse/apps/log-viewer/kubeconfig -t value -v "$(cat <<EOKUBECONFIG'
echo "${kubeconfig}"
echo 'EOKUBECONFIG'
echo ')"'
echo
echo "echo \$KUBECONFIG > k"
echo "export KUBECONFIG=k"
echo "kubectl get all"
