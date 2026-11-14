package k8s

import "strings"

type ClusterKind string

const (
	KindCluster     ClusterKind = "kind"
	K3dCluster      ClusterKind = "k3d"
	MinikubeCluster ClusterKind = "minikube"
	SharedCluster   ClusterKind = "shared"
	RemoteCluster   ClusterKind = "remote"
)

type Cluster struct {
	Kind ClusterKind
	Name string
}

func (c Cluster) Local() bool { return c.Kind != RemoteCluster }

func Classify(context string) Cluster {
	switch {
	case strings.HasPrefix(context, "kind-"):
		return Cluster{Kind: KindCluster, Name: strings.TrimPrefix(context, "kind-")}
	case strings.HasPrefix(context, "k3d-"):
		return Cluster{Kind: K3dCluster, Name: strings.TrimPrefix(context, "k3d-")}
	case context == "minikube":
		return Cluster{Kind: MinikubeCluster, Name: "minikube"}
	}
	switch context {
	case "docker-desktop", "docker-for-desktop", "orbstack", "rancher-desktop", "colima":
		return Cluster{Kind: SharedCluster, Name: context}
	}
	return Cluster{Kind: RemoteCluster, Name: context}
}

func (c Cluster) hostAlias() string {
	if c.Kind == MinikubeCluster {
		return "host.minikube.internal"
	}
	return "host.docker.internal"
}

func (c Cluster) loadImage(image string) []string {
	switch c.Kind {
	case KindCluster:
		return []string{"kind", "load", "docker-image", image, "--name", c.Name}
	case K3dCluster:
		return []string{"k3d", "image", "import", image, "-c", c.Name}
	case MinikubeCluster:
		return []string{"minikube", "-p", c.Name, "image", "load", image}
	}
	return nil
}

func (c Cluster) startHint() string {
	switch c.Kind {
	case KindCluster:
		return "kind create cluster --name " + c.Name
	case K3dCluster:
		return "k3d cluster create " + c.Name
	case MinikubeCluster:
		return "minikube start"
	case SharedCluster:
		return "turn Kubernetes on in " + c.Name + "'s settings"
	}
	return "kubectl --context " + c.Name + " cluster-info"
}
