package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

//go:embed static
var staticFiles embed.FS

// ---- response types ----

type NodeInfo struct {
	Name   string   `json:"name"`
	Roles  []string `json:"roles"`
	Ready  bool     `json:"ready"`
	CPUCap string   `json:"cpuCapacity"`
	MemCap string   `json:"memCapacity"`
}

type NodeMetrics struct {
	Name string `json:"name"`
	CPU  string `json:"cpu"`
	Mem  string `json:"memory"`
}

type PodInfo struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase"`
	Restarts  int32  `json:"restarts"`
	Node      string `json:"node"`
}

type DeploymentInfo struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Desired   int32  `json:"desired"`
	Ready     int32  `json:"ready"`
}

type MetricsResponse struct {
	Nodes       []NodeInfo       `json:"nodes"`
	Pods        []PodInfo        `json:"pods"`
	Deployments []DeploymentInfo `json:"deployments"`
	NodeMetrics []NodeMetrics    `json:"nodeMetrics,omitempty"`
}

// ---- main ----

func main() {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("in-cluster config: %v", err)
	}

	k8s, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("k8s client: %v", err)
	}

	var mc *metricsclient.Clientset
	mc, err = metricsclient.NewForConfig(config)
	if err != nil {
		log.Printf("metrics client unavailable: %v", err)
		mc = nil
	}

	static, _ := fs.Sub(staticFiles, "static")
	http.Handle("/", http.FileServer(http.FS(static)))
	http.HandleFunc("/api/metrics", metricsHandler(k8s, mc))

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// ---- handler ----

func metricsHandler(k8s kubernetes.Interface, mc *metricsclient.Clientset) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		resp := MetricsResponse{}

		if nodes, err := k8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
			for _, n := range nodes.Items {
				resp.Nodes = append(resp.Nodes, buildNodeInfo(n))
			}
		}

		if pods, err := k8s.CoreV1().Pods("").List(ctx, metav1.ListOptions{}); err == nil {
			for _, p := range pods.Items {
				resp.Pods = append(resp.Pods, buildPodInfo(p))
			}
		}

		if deps, err := k8s.AppsV1().Deployments("").List(ctx, metav1.ListOptions{}); err == nil {
			for _, d := range deps.Items {
				resp.Deployments = append(resp.Deployments, buildDeploymentInfo(d))
			}
		}

		if mc != nil {
			if nm, err := mc.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{}); err == nil {
				for _, m := range nm.Items {
					resp.NodeMetrics = append(resp.NodeMetrics, buildNodeMetrics(m))
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// ---- builders ----

func buildNodeInfo(n corev1.Node) NodeInfo {
	info := NodeInfo{
		Name:   n.Name,
		CPUCap: n.Status.Capacity.Cpu().String(),
		MemCap: n.Status.Capacity.Memory().String(),
	}
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			info.Ready = c.Status == corev1.ConditionTrue
		}
	}
	for k := range n.Labels {
		switch k {
		case "node-role.kubernetes.io/control-plane", "node-role.kubernetes.io/master":
			info.Roles = append(info.Roles, "control-plane")
		case "node-role.kubernetes.io/worker":
			info.Roles = append(info.Roles, "worker")
		}
	}
	if len(info.Roles) == 0 {
		info.Roles = []string{"worker"}
	}
	return info
}

func buildPodInfo(p corev1.Pod) PodInfo {
	var restarts int32
	for _, cs := range p.Status.ContainerStatuses {
		restarts += cs.RestartCount
	}
	return PodInfo{
		Name:      p.Name,
		Namespace: p.Namespace,
		Phase:     string(p.Status.Phase),
		Restarts:  restarts,
		Node:      p.Spec.NodeName,
	}
}

func buildDeploymentInfo(d appsv1.Deployment) DeploymentInfo {
	desired := int32(0)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return DeploymentInfo{
		Name:      d.Name,
		Namespace: d.Namespace,
		Desired:   desired,
		Ready:     d.Status.ReadyReplicas,
	}
}

func buildNodeMetrics(m metricsv1beta1.NodeMetrics) NodeMetrics {
	return NodeMetrics{
		Name: m.Name,
		CPU:  m.Usage.Cpu().String(),
		Mem:  m.Usage.Memory().String(),
	}
}
