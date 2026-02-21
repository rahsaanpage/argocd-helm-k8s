# argocd-helm-k8s

A GitOps demo that deploys a simple nginx website to a **kind** cluster using **Helm** and **ArgoCD**.

## Prerequisites

| Tool | Version |
|------|---------|
| [kind](https://kind.sigs.k8s.io/) | >= 0.20 |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | >= 1.27 |
| [helm](https://helm.sh/docs/intro/install/) | >= 3.12 |
| [argocd CLI](https://argo-cd.readthedocs.io/en/stable/cli_installation/) | >= 2.9 (optional) |

---

## Project Structure

```
argocd-helm-k8s/
├── argocd/
│   └── application.yaml          # ArgoCD Application manifest
└── charts/
    └── simple-website/
        ├── Chart.yaml
        ├── values.yaml
        └── templates/
            ├── _helpers.tpl
            ├── configmap.yaml    # HTML page content
            ├── deployment.yaml
            ├── service.yaml
            └── ingress.yaml
```

---

## Quick Start

### 1. Create a kind cluster

```bash
kind create cluster --name demo
```

### 2. Install ingress-nginx

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.10.1/deploy/static/provider/kind/deploy.yaml

kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=90s
```

### 3. Install ArgoCD

```bash
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

kubectl wait --namespace argocd \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/name=argocd-server \
  --timeout=120s
```

### 4. Push this repo to GitHub

ArgoCD must be able to reach the Git repository.

```bash
git remote add origin https://github.com/<your-username>/argocd-helm-k8s.git
git push -u origin main
```

Update the `repoURL` in [argocd/application.yaml](argocd/application.yaml) with your actual repo URL.

### 5. Apply the ArgoCD Application

```bash
kubectl apply -f argocd/application.yaml
```

ArgoCD will clone the repo, render the Helm chart, and deploy it automatically.

### 6. Add a local DNS entry

```bash
echo "127.0.0.1  simple-website.local" | sudo tee -a /etc/hosts
```

### 7. Open the site

```
http://simple-website.local
```

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

## Making Changes

Edit any file under `charts/simple-website/`, commit, and push. ArgoCD detects the change
and syncs within ~3 minutes (default poll interval). To sync immediately:

```bash
argocd app sync simple-website
# or via kubectl
kubectl patch app simple-website -n argocd \
  --type merge -p '{"operation":{"initiatedBy":{"username":"admin"},"sync":{}}}'
```

---

## Cleanup

```bash
kubectl delete -f argocd/application.yaml
kind delete cluster --name demo
```

---

## Architecture

See [docs/architecture-overview.md](docs/architecture-overview.md) for a detailed diagram and component breakdown.