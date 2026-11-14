package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/k8s"
	"github.com/spf13/cobra"
)

var k8sCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Services that run in Kubernetes: up, down, the manifests corgi generates",
	Long: `A service with runner: {name: k8s} runs in a cluster instead of on this
machine. corgi run applies it, waits for the rollout, forwards its port to
localhost and streams its logs; stopping removes what it applied.

  services:
    api:
      port: 3000
      runner:
        name: k8s
        kubeContext: kind-dev      # needed for any cluster that is not local
        manifests: deploy/k8s      # or chart: deploy/chart, or neither: corgi
                                   # builds the Dockerfile and writes a Deployment

  corgi k8s manifests api   # what corgi would apply, to start your own from
  corgi k8s status          # every k8s service: context, namespace, rollout`,
}

var k8sUpCmd = &cobra.Command{
	Use:   "up <service>",
	Short: "Apply a service to its cluster, forward its port and follow its logs until stopped",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		session := mustK8sSession(cmd, args[0])
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err := session.Up(ctx)
		if err == nil {
			session.Serve(ctx)
		}
		keep, _ := cmd.Flags().GetBool("keep")
		if !keep {
			downCtx, cancel := k8s.StopContext()
			if derr := session.Down(downCtx); derr != nil {
				fmt.Fprintln(os.Stderr, "☸", args[0], "cleanup:", derr)
			}
			cancel()
		}
		if err != nil {
			exitWithError("k8s_up", err, 1)
		}
	},
}

var k8sDownCmd = &cobra.Command{
	Use:   "down <service>",
	Short: "Remove what corgi applied for a service",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		session := mustK8sSession(cmd, args[0])
		ctx, cancel := k8s.StopContext()
		defer cancel()
		if err := session.Down(ctx); err != nil {
			exitWithError("k8s_down", err, 1)
		}
	},
}

var k8sManifestsCmd = &cobra.Command{
	Use:   "manifests <service>",
	Short: "Print the Deployment and Service corgi generates for a service",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		session := mustK8sSession(cmd, args[0])
		spec := session.Spec
		if spec.Source() != "generated" {
			fmt.Printf("# %s uses its own %s: %s\n", spec.Service, spec.Source(), firstNonEmpty(spec.Chart, spec.Manifests))
			return
		}
		image := spec.Image
		if image == "" {
			image = "corgi/" + k8s.DNSName(spec.Stack+"-"+spec.Name) + ":<built on corgi run>"
		}
		os.Stdout.Write(spec.GeneratedYAML(image))
	},
}

var k8sStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Every k8s service: its context, namespace and rollout",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		corgi := mustLoadCompose(cmd)
		var rows []map[string]any
		for _, svc := range k8sServices(corgi) {
			spec, err := k8s.FromService(svc, stackName(corgi))
			row := map[string]any{"service": svc.ServiceName}
			if err != nil {
				row["error"] = err.Error()
				rows = append(rows, row)
				continue
			}
			session := k8s.NewSession(spec, k8s.System(), os.Stderr)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			row["context"], row["namespace"], row["source"] = spec.KubeContext, spec.Namespace, spec.Source()
			if err := session.ResolveContext(ctx); err != nil {
				row["error"] = err.Error()
			} else {
				row["context"] = session.Spec.KubeContext
				out, _ := session.Exec.Run(ctx, nil, "kubectl", "--context", session.Spec.KubeContext, "get", "pods", "-n", spec.Namespace, "-l", spec.Selector(), "--no-headers")
				row["pods"] = strings.TrimSpace(string(out))
			}
			cancel()
			rows = append(rows, row)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"services": rows})
			return
		}
		if len(rows) == 0 {
			fmt.Println("no service here has runner: k8s")
			return
		}
		for _, r := range rows {
			fmt.Printf("%-16s %v/%v (%v)\n", r["service"], r["context"], r["namespace"], r["source"])
			if e, ok := r["error"]; ok {
				fmt.Printf("  %v\n", e)
			} else if p := r["pods"]; p != "" {
				fmt.Printf("  %v\n", p)
			} else {
				fmt.Println("  no pods (not running, or its manifests use other labels)")
			}
		}
	},
}

func mustLoadCompose(cmd *cobra.Command) *utils.CorgiCompose {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		exitWithError("k8s_config", err, 2)
	}
	return corgi
}

func stackName(corgi *utils.CorgiCompose) string {
	if corgi.Name != "" {
		return corgi.Name
	}
	return filepath.Base(utils.CorgiComposePathDir)
}

func k8sServices(corgi *utils.CorgiCompose) []utils.Service {
	var out []utils.Service
	for _, s := range corgi.Services {
		if s.Runner.IsK8s() {
			out = append(out, s)
		}
	}
	return out
}

func mustK8sSession(cmd *cobra.Command, name string) *k8s.Session {
	corgi := mustLoadCompose(cmd)
	for _, svc := range corgi.Services {
		if svc.ServiceName != name {
			continue
		}
		if !svc.Runner.IsK8s() {
			exitWithError("k8s_not_k8s", fmt.Errorf("%s does not have runner: k8s", name), 2)
		}
		spec, err := k8s.FromService(svc, stackName(corgi))
		if err != nil {
			exitWithError("k8s_spec", err, 2)
		}
		return k8s.NewSession(spec, k8s.System(), os.Stdout)
	}
	exitWithError("k8s_no_service", fmt.Errorf("no service %s in %s", name, utils.CorgiComposePath), 2)
	return nil
}

func k8sUpCommand(svc utils.Service) string {
	exe, err := os.Executable()
	if err != nil {
		exe = "corgi"
	}
	return shellQuote(exe) + " k8s up " + shellQuote(svc.ServiceName) + " --filename " + shellQuote(utils.CorgiComposePath)
}

func withK8sStart(svc utils.Service) utils.Service {
	if svc.Runner.IsK8s() {
		svc.Start = []string{k8sUpCommand(svc)}
	}
	return svc
}

func stopK8sRunners(corgi *utils.CorgiCompose, only ...string) {
	if corgi == nil {
		return
	}
	for _, svc := range k8sServices(corgi) {
		if len(only) > 0 && !containsFold(only, svc.ServiceName) {
			continue
		}
		spec, err := k8s.FromService(svc, stackName(corgi))
		if err != nil {
			continue
		}
		ctx, cancel := k8s.StopContext()
		if err := k8s.NewSession(spec, k8s.System(), utils.ConsoleOut()).Down(ctx); err != nil {
			utils.Infof("⚠ could not remove k8s service %s: %v\n", svc.ServiceName, err)
		}
		cancel()
	}
}

var k8sLookPath = exec.LookPath

func k8sToolChecks(corgi *utils.CorgiCompose) []doctorCheck {
	need := map[string]string{}
	for _, svc := range k8sServices(corgi) {
		need["kubectl"] = "brew install kubectl, or kubernetes.io/docs/tasks/tools"
		if svc.Runner.Chart != "" {
			need["helm"] = "brew install helm"
		}
		spec, err := k8s.FromService(svc, stackName(corgi))
		if err != nil || spec.Build == nil {
			continue
		}
		need["docker"] = "Docker Desktop, OrbStack or colima"
		switch k8s.Classify(spec.KubeContext).Kind {
		case k8s.KindCluster:
			need["kind"] = "brew install kind"
		case k8s.K3dCluster:
			need["k3d"] = "brew install k3d"
		case k8s.MinikubeCluster:
			need["minikube"] = "brew install minikube"
		}
	}
	tools := make([]string, 0, len(need))
	for tool := range need {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	var checks []doctorCheck
	for _, tool := range tools {
		c := doctorCheck{Name: "k8s:" + tool, OK: true}
		if _, err := k8sLookPath(tool); err != nil {
			c.OK, c.Detail = false, "not found - "+need[tool]
		}
		checks = append(checks, c)
	}
	return checks
}

func init() {
	k8sUpCmd.Flags().Bool("keep", false, "Leave what was applied in the cluster when stopped")
	k8sCmd.AddCommand(k8sUpCmd, k8sDownCmd, k8sManifestsCmd, k8sStatusCmd)
	rootCmd.AddCommand(k8sCmd)
}
