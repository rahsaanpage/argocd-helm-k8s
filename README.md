# argocd-helm-k8s

A GitOps demo that deploys a **live Kubernetes cluster metrics dashboard** to a **kind** cluster using a custom Go image, **Helm**, and **ArgoCD**.

The dashboard shows real-time pods, nodes, deployments, and CPU/memory usage — auto-refreshing every 10 seconds, served by a Go HTTP server that queries the Kubernetes API directly.

## Prerequisites

| Tool | Version |
|------|---------|
| [kind](https://kind.sigs.k8s.io/) | >= 0.20 |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | >= 1.27 |
| [helm](https://helm.sh/docs/intro/install/) | >= 3.12 |
| [docker](https://docs.docker.com/get-docker/) | >= 24 |
| [argocd CLI](https://argo-cd.readthedocs.io/en/stable/cli_installation/) | >= 2.9 (optional) |

---

## Project Structure

```
argocd-helm-k8s/
├── app/                              # Go metrics server
│   ├── main.go                       # HTTP server: /api/metrics + embedded dashboard
│   ├── go.mod                        # k8s.io/client-go, k8s.io/metrics
│   ├── Dockerfile                    # Multi-stage: golang:1.22-alpine → alpine:3.19
│   └── static/
│       └── index.html                # Dark-themed dashboard UI (auto-refreshes)
├── argocd/
│   └── application.yaml              # ArgoCD Application CRD
└── charts/
    └── simple-website/
        ├── Chart.yaml
        ├── values.yaml
        └── templates/
            ├── _helpers.tpl
            ├── deployment.yaml           # Runs the Go server (port 8080)
            ├── service.yaml              # ClusterIP on port 80
            ├── ingress.yaml              # Routes simple-website.local
            ├── serviceaccount.yaml       # Identity for the pod
            ├── clusterrole.yaml          # Read access to pods/nodes/deployments/metrics
            └── clusterrolebinding.yaml   # Binds role to ServiceAccount
```

---

## Quick Start

### 1. Create a kind cluster

```bash
kind create cluster --name demo
```

### 2. Set up a local image registry

The Go server image is pushed to a local registry accessible from within kind nodes.

```bash
# Start the registry
docker run -d --restart=always -p 5001:5000 --name registry registry:2
docker network connect kind registry

# Configure containerd on each kind node to mirror localhost:5001 → registry:5000
for node in $(kind get nodes --name demo); do
  docker exec "$node" sh -c "
    mkdir -p /etc/containerd/certs.d/localhost:5001
    cat > /etc/containerd/certs.d/localhost:5001/hosts.toml <<'EOF'
server = \"http://registry:5000\"
[host.\"http://registry:5000\"]
  capabilities = [\"pull\", \"resolve\"]
EOF
    printf '\n[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\"\n' \
      >> /etc/containerd/config.toml
    systemctl restart containerd
  "
done
```

### 3. Build and push the dashboard image

```bash
docker build -t localhost:5001/simple-website:latest app/
docker push localhost:5001/simple-website:latest
```

### 4. Install ingress-nginx

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.10.1/deploy/static/provider/kind/deploy.yaml
```

> **kind requirement:** Label a worker node so the ingress controller can schedule:
>
> ```bash
> kubectl label node <your-worker-node> ingress-ready=true
> ```

```bash
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=90s
```

### 5. Install ArgoCD

```bash
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

kubectl wait --namespace argocd \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/name=argocd-server \
  --timeout=120s
```

### 6. Push this repo to GitHub

ArgoCD must be able to reach the Git repository.

```bash
git remote add origin https://github.com/<your-username>/argocd-helm-k8s.git
git push -u origin main
```

Update the `repoURL` in [argocd/application.yaml](argocd/application.yaml) with your actual repo URL.

### 7. Apply the ArgoCD Application

```bash
kubectl apply -f argocd/application.yaml
```

ArgoCD will clone the repo, render the Helm chart, and deploy the dashboard automatically.

### 8. Access the dashboard

```bash
kubectl port-forward svc/simple-website 9090:80 -n default
```

Open **http://localhost:9090** — you'll see a live metrics dashboard for your cluster.

---

## Making Changes

### Helm / config changes (values, replicas, ingress, etc.)

Edit files under `charts/simple-website/`, commit, and push. ArgoCD detects the change and syncs within ~3 minutes. To sync immediately:

```bash
kubectl patch app simple-website -n argocd \
  --type merge -p '{"operation":{"initiatedBy":{"username":"admin"},"sync":{}}}'
```

### App code changes (`app/`)

After editing `app/main.go` or `app/static/index.html`, rebuild and push the image, then trigger a rollout:

```bash
docker build -t localhost:5001/simple-website:latest app/
docker push localhost:5001/simple-website:latest
kubectl rollout restart deployment/simple-website -n default
```

> ArgoCD uses `imagePullPolicy: Always` so each rollout pulls the latest image from the local registry.

---

## Accessing the ArgoCD UI

```bash
# Get the initial admin password
kubectl get secret argocd-initial-admin-secret -n argocd \
  -o jsonpath="{.data.password}" | base64 -d; echo

# Port-forward the ArgoCD server
kubectl port-forward svc/argocd-server -n argocd 8080:443
```

Then open https://localhost:8080 (user: `admin`).

---

## Cleanup

```bash
kubectl delete -f argocd/application.yaml
kind delete cluster --name demo
docker rm -f registry
```

---

## Architecture

See [docs/architecture-overview.md](docs/architecture-overview.md) for a detailed diagram and component breakdown.
