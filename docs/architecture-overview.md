# Architecture Overview

## Goal

Deploy a live **Kubernetes cluster metrics dashboard** using a fully GitOps workflow:
**Git → ArgoCD → Helm → kind cluster → ingress-nginx → browser**

The dashboard is served by a custom Go HTTP server that queries the Kubernetes API in real time and renders pod, node, deployment, and CPU/memory metrics in a dark-themed UI that auto-refreshes every 10 seconds.

---

## High-Level Flow

```
┌─────────────────────────────────────────────────────────────┐
│                        Developer                            │
│                                                             │
│   edit files  →  git commit  →  git push                   │
│   (app code change: also rebuild + docker push)             │
└───────────────────────────┬─────────────────────────────────┘
                            │  (GitHub)
                            ▼
                    ┌───────────────┐
                    │   Git Remote  │  source of truth
                    │  (repository) │
                    └───────┬───────┘
                            │  ArgoCD polls ~3 min
                            ▼
┌──────────────────────────────────────────────────────────────────┐
│                        kind Cluster                              │
│                                                                  │
│  ┌─────────────────────────────────────────┐                    │
│  │  namespace: argocd                       │                    │
│  │   ┌──────────────────────┐               │                    │
│  │   │  ArgoCD Controller   │  renders Helm │                    │
│  │   │  + Repo Server       │  applies diffs│                    │
│  │   └──────────────────────┘               │                    │
│  └─────────────────────────────────────────┘                    │
│                                                                  │
│  ┌─────────────────────────────────────────┐                    │
│  │  namespace: default                      │                    │
│  │                                          │                    │
│  │   ┌──────────────────────────────────┐  │                    │
│  │   │  Deployment (2 replicas)         │  │                    │
│  │   │  image: localhost:5001/          │  │                    │
│  │   │         simple-website:latest    │  │                    │
│  │   │                                  │  │                    │
│  │   │  Go HTTP server (:8080)          │  │                    │
│  │   │  ├── GET /          → dashboard  │  │                    │
│  │   │  └── GET /api/metrics → JSON     │  │                    │
│  │   │       ├── pods (all namespaces)  │  │                    │
│  │   │       ├── nodes                  │  │                    │
│  │   │       ├── deployments            │  │                    │
│  │   │       └── node metrics (CPU/mem) │  │                    │
│  │   └──────────────┬───────────────────┘  │                    │
│  │                  │  ServiceAccount       │                    │
│  │                  │  + ClusterRole        │                    │
│  │           ┌──────▼──────┐               │                    │
│  │           │   Service   │               │                    │
│  │           │ (ClusterIP) │               │                    │
│  │           │  port 80    │               │                    │
│  │           └──────┬──────┘               │                    │
│  └──────────────────┼───────────────────── ┘                    │
│                     │                                            │
│  ┌──────────────────┼──────────────────────┐                    │
│  │  namespace: ingress-nginx               │                    │
│  │           ┌──────▼──────┐               │                    │
│  │           │   Ingress   │               │                    │
│  │           │  Controller │               │                    │
│  │           └──────┬──────┘               │                    │
│  └──────────────────┼───────────────────── ┘                    │
│                     │                                            │
│  ┌──────────────────┼──────────────────────┐                    │
│  │  host Docker network                    │                    │
│  │       ┌──────────▼──────────┐           │                    │
│  │       │  Local Registry     │           │                    │
│  │       │  registry:5000      │           │                    │
│  │       │  (localhost:5001)   │           │                    │
│  │       └─────────────────────┘           │                    │
│  └─────────────────────────────────────────┘                    │
└──────────────────────────────────────────────────────────────────┘
                     │  port-forward 9090:80
                     ▼
             ┌───────────────┐
             │    Browser    │
             │ localhost:9090│
             └───────────────┘
```

---

## Components

### Git Repository (Source of Truth)
The entire desired cluster state lives here. The Helm chart and ArgoCD Application manifest fully describe what runs in the cluster. The `app/` directory holds the Go source and Dockerfile — changes there require a `docker build + push` in addition to a `git push`.

### ArgoCD
| Component | Role |
|-----------|------|
| **Application** CRD | Declares repo URL, chart path, target namespace, and sync policy |
| **Application Controller** | Polls Git, diffs rendered manifests vs. live state, applies changes |
| **Repo Server** | Clones the repo and renders Helm templates with values.yaml |

Sync policy is **automated** with `prune: true` and `selfHeal: true`:
- Resources deleted from Git are pruned from the cluster.
- Manual `kubectl` changes are reverted automatically.

### Helm Chart (`charts/simple-website`)

| Template | Purpose |
|----------|---------|
| `deployment.yaml` | Runs 2 replicas of the Go dashboard server (port 8080); `revisionHistoryLimit: 2` |
| `service.yaml` | ClusterIP exposing port 80 → pod port 8080 |
| `ingress.yaml` | Routes `simple-website.local` to the service via ingress-nginx |
| `serviceaccount.yaml` | Pod identity used for Kubernetes API calls |
| `clusterrole.yaml` | `get`/`list` on pods, nodes, deployments, and `metrics.k8s.io` |
| `clusterrolebinding.yaml` | Binds the ClusterRole to the ServiceAccount |

### Go Metrics Server (`app/`)

A small HTTP server built with `k8s.io/client-go` and `k8s.io/metrics`. It uses in-cluster config to authenticate, then exposes:

- `GET /` — serves the embedded `static/index.html` dashboard
- `GET /api/metrics` — returns JSON containing nodes, pods, deployments, and (if metrics-server is installed) live CPU/memory usage

The HTML/JS dashboard fetches `/api/metrics` every 10 seconds and updates the DOM without a page reload.

### Local Docker Registry

Because kind nodes cannot pull from `localhost` on the host, a Docker container named `registry` is connected to the kind Docker network. Containerd on each kind node is configured to mirror `localhost:5001` → `http://registry:5000`. This allows `imagePullPolicy: Always` to work without an external registry.

### ingress-nginx
Acts as the cluster edge proxy. The kind-specific manifest binds the controller's hostPort to `127.0.0.1:80/443`, making it reachable from the host without a cloud load balancer.

### kind (Kubernetes in Docker)
Runs a full multi-node Kubernetes cluster inside Docker containers. Ideal for local development and CI/CD testing.

---

## GitOps Sync Lifecycle

```
git push (Helm/config change)
    │
    ▼  (poll ~3 min or manual patch)
ArgoCD detects diff
    │
    ▼
Repo Server clones repo + renders Helm chart
    │
    ▼
Application Controller diffs vs. live cluster state
    │
    ├── No diff  →  Status: Synced ✅
    │
    └── Diff     →  Apply manifests
                         │
                         ▼
                   Status: Synced ✅  (or Degraded ❌ if pod fails)

docker build + push (app code change)
    │
    ▼
kubectl rollout restart deployment/simple-website
    │
    ▼
Pods pull localhost:5001/simple-website:latest from local registry
```

---

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| Custom Go image instead of nginx + ConfigMap | Enables live Kubernetes API queries; static HTML cannot call the cluster API |
| Local kind registry (`localhost:5001`) | No external registry account needed; works fully offline |
| Multi-stage Docker build (golang → alpine) | Final image is ~26 MB; no Go toolchain in production image |
| In-cluster RBAC (ClusterRole + ServiceAccount) | Least-privilege access; pod reads only the resources it needs |
| Graceful metrics fallback | If `metrics-server` is not installed, CPU/memory columns show "n/a" rather than crashing |
| `imagePullPolicy: Always` | Ensures pods always pull the latest push from the local registry on restart |
| ArgoCD `automated` sync | True GitOps — cluster state always reflects the repo |
| `prune: true` | Removes orphaned resources when files are deleted from Git |
| `selfHeal: true` | Reverts drift from manual `kubectl` edits |
| `revisionHistoryLimit: 2` | Caps stale ReplicaSets to avoid accumulation |
| ingress-nginx over NodePort | Single entry point; host-based routing scales to multiple apps |

---

## Extending This Setup

- **Multiple apps** — add new `charts/<app-name>/` directories and a new `argocd/<app-name>.yaml` Application.
- **metrics-server** — install with `--kubelet-insecure-tls` for kind to enable live CPU/memory data.
- **Environments** — use Helm value overrides per environment (`values-prod.yaml`) or separate ArgoCD Applications per namespace.
- **Secrets** — integrate [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets) or [External Secrets Operator](https://external-secrets.io/) to store secrets safely in Git.
- **CI pipeline** — add a GitHub Actions workflow that runs `helm lint`, `helm template`, and `docker build` on pull requests before merge.
- **External registry** — swap `localhost:5001/simple-website` for a real registry (Docker Hub, ghcr.io) and update `values.yaml` accordingly.
