# Architecture Overview

## Goal

Deploy a static website to Kubernetes using a fully GitOps workflow:
**Git → ArgoCD → Helm → kind cluster → ingress-nginx → browser**

---

## High-Level Flow

```
┌─────────────────────────────────────────────────────────────┐
│                        Developer                            │
│                                                             │
│   edit files  →  git commit  →  git push                   │
└───────────────────────────┬─────────────────────────────────┘
                            │  (GitHub / GitLab)
                            ▼
                    ┌───────────────┐
                    │   Git Remote  │  source of truth
                    │  (repository) │
                    └───────┬───────┘
                            │  ArgoCD polls / webhook
                            ▼
┌───────────────────────────────────────────────────────────────────┐
│                      kind Cluster                                 │
│                                                                   │
│  ┌──────────────────────────────────────────┐                    │
│  │  namespace: argocd                        │                    │
│  │                                           │                    │
│  │   ┌───────────────┐                       │                    │
│  │   │  ArgoCD       │  1. detects Git diff  │                    │
│  │   │  Application  │  2. renders Helm chart│                    │
│  │   │  Controller   │  3. applies manifests │                    │
│  │   └───────────────┘                       │                    │
│  └──────────────────────────────────────────┘                    │
│                                                                   │
│  ┌──────────────────────────────────────────┐                    │
│  │  namespace: default                       │                    │
│  │                                           │                    │
│  │   ┌────────────┐     ┌─────────────────┐ │                    │
│  │   │ ConfigMap  │     │   Deployment    │ │                    │
│  │   │ index.html │────▶│  (nginx:alpine) │ │                    │
│  │   └────────────┘     └────────┬────────┘ │                    │
│  │                               │           │                    │
│  │                        ┌──────▼──────┐   │                    │
│  │                        │   Service   │   │                    │
│  │                        │ (ClusterIP) │   │                    │
│  │                        └──────┬──────┘   │                    │
│  └───────────────────────────────┼──────────┘                    │
│                                  │                                │
│  ┌───────────────────────────────┼──────────┐                    │
│  │  namespace: ingress-nginx     │           │                    │
│  │                        ┌──────▼──────┐   │                    │
│  │                        │   Ingress   │   │                    │
│  │                        │  Controller │   │                    │
│  │                        │ (nginx-nginx)│  │                    │
│  │                        └──────┬──────┘   │                    │
│  └───────────────────────────────┼──────────┘                    │
└──────────────────────────────────┼────────────────────────────────┘
                                   │  port 80
                                   ▼
                           ┌───────────────┐
                           │    Browser    │
                           │ simple-website│
                           │    .local     │
                           └───────────────┘
```

---

## Components

### Git Repository (Source of Truth)
The entire desired state of the cluster lives here. No manual `kubectl apply` is needed after initial bootstrap — pushing a commit is the only deployment mechanism.

### ArgoCD
| Component | Role |
|-----------|------|
| **Application** CRD | Declares what repo/path/cluster/namespace to sync |
| **Application Controller** | Watches Git, diffs desired vs. live state, applies changes |
| **Repo Server** | Clones the repo and renders Helm templates |

Sync policy is set to **automated** with `prune: true` and `selfHeal: true`, meaning:
- Resources deleted from Git are pruned from the cluster.
- Manual changes to live objects are reverted automatically.

### Helm Chart (`charts/simple-website`)
| Template | Purpose |
|----------|---------|
| `configmap.yaml` | Stores `index.html`; avoids building a custom Docker image |
| `deployment.yaml` | Runs `nginx:alpine`; mounts ConfigMap as `/usr/share/nginx/html` |
| `service.yaml` | ClusterIP service exposing port 80 within the cluster |
| `ingress.yaml` | Routes `simple-website.local` → service via ingress-nginx |

### ingress-nginx
Acts as the cluster's edge proxy. Deployed with the kind-specific manifest which binds the controller's hostPort to `127.0.0.1:80/443`, making it reachable from the host machine without a cloud load balancer.

### kind (Kubernetes in Docker)
Runs a full Kubernetes cluster inside Docker containers on the local machine. Ideal for local development and CI.

---

## GitOps Sync Lifecycle

```
git push
    │
    ▼  (poll ~3 min or webhook)
ArgoCD detects diff
    │
    ▼
Repo Server clones repo
    │
    ▼
Helm renders templates (values.yaml merged)
    │
    ▼
Application Controller diffs rendered manifests vs. live cluster state
    │
    ├── No diff  →  Status: Synced ✅
    │
    └── Diff     →  Apply manifests
                         │
                         ▼
                   Status: Synced ✅  (or Degraded ❌ if pod fails)
```

---

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| HTML via ConfigMap, not custom image | No registry needed; content changes are a pure Git operation |
| `nginx:alpine` base image | Minimal attack surface, small image size (~8 MB) |
| ArgoCD `automated` sync | True GitOps — cluster state always reflects the repo |
| `prune: true` | Removes orphaned resources when files are deleted from Git |
| `selfHeal: true` | Reverts any drift from manual `kubectl` edits |
| ingress-nginx over NodePort | Single entry point; host-based routing scales to multiple apps |

---

## Extending This Setup

- **Multiple apps** — add new `charts/<app-name>/` directories and a new `argocd/<app-name>.yaml` Application.
- **Environments** — use Helm value overrides per environment (`values-prod.yaml`) or separate ArgoCD Applications per namespace.
- **Secrets** — integrate [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets) or [External Secrets Operator](https://external-secrets.io/) to store secrets safely in Git.
- **CI pipeline** — add a GitHub Actions workflow that runs `helm lint` and `helm template` on pull requests before merge.