package cli

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yanjiulab/netnslab/internal/config"
	"github.com/yanjiulab/netnslab/internal/labstate"
	"github.com/yanjiulab/netnslab/internal/logx"
	"github.com/yanjiulab/netnslab/internal/mgmt"
	"github.com/yanjiulab/netnslab/internal/netns"
	"github.com/yanjiulab/netnslab/internal/routing"
	"github.com/yanjiulab/netnslab/internal/topology"
)

func logDeployStage(stage string, start time.Time) {
	logx.S().Debugw("deploy stage done", "stage", stage, "elapsed", time.Since(start).String())
}

func NewDeployCommand() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a network lab from a YAML topology file",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug := viper.GetBool("debug")
			if err := logx.Init(debug); err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			if file == "" {
				return fmt.Errorf("config file must be specified with -f")
			}

			cfg, err := config.LoadConfig(file)
			if err != nil {
				return err
			}
			labName := cfg.Name
			if _, err := os.Stat(netns.LabRunDir(labName)); err == nil {
				return fmt.Errorf("lab %q already deployed; run `netnslab destroy %s` first", labName, labName)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("deploy/precheck: stat lab run dir: %w", err)
			}

			if _, err := topology.Build(cfg); err != nil {
				return fmt.Errorf("deploy/build topology: %w", err)
			}
			if err := topology.AllocateAddresses(cfg); err != nil {
				return fmt.Errorf("deploy/allocate addresses: %w", err)
			}

			nsStart := time.Now()
			for nodeName := range cfg.Topology.Nodes {
				if err := netns.EnsureLabDirs(labName, nodeName); err != nil {
					return fmt.Errorf("deploy/create ns %s: ensure dirs: %w", nodeName, err)
				}
				if err := netns.CreateNamespace(labName, nodeName); err != nil {
					return fmt.Errorf("deploy/create ns %s: create namespace: %w", nodeName, err)
				}
				if err := netns.EnsureLoopbackUp(labName, nodeName); err != nil {
					return fmt.Errorf("deploy/create ns %s: set loopback up: %w", nodeName, err)
				}
			}
			logDeployStage("create ns", nsStart)

			linkStart := time.Now()
			for _, link := range cfg.Topology.Links {
				nodeA, ifA := config.SplitEndpointPublic(link.Endpoints[0])
				nodeB, ifB := config.SplitEndpointPublic(link.Endpoints[1])

				if err := netns.CreateVethPair(labName, nodeA, ifA, nodeB, ifB); err != nil {
					return fmt.Errorf("deploy/link %s:%s <-> %s:%s: create veth pair: %w", nodeA, ifA, nodeB, ifB, err)
				}

				ifaceA := cfg.Topology.Nodes[nodeA].Interfaces[ifA]
				ifaceB := cfg.Topology.Nodes[nodeB].Interfaces[ifB]

				if err := netns.ConfigureLinkInterface(labName, nodeA, ifA, ifaceA.IP); err != nil {
					return fmt.Errorf("deploy/link %s:%s: configure interface: %w", nodeA, ifA, err)
				}
				if err := netns.ConfigureLinkInterface(labName, nodeB, ifB, ifaceB.IP); err != nil {
					return fmt.Errorf("deploy/link %s:%s: configure interface: %w", nodeB, ifB, err)
				}
			}
			logDeployStage("link", linkStart)

			// Linux bridge inside each kind=bridge netns: L2 between all bridge ports.
			for nodeName, n := range cfg.Topology.Nodes {
				if n.Kind != "bridge" {
					continue
				}
				ports := topology.BridgePortIfaces(cfg, nodeName)
				if err := netns.SetupLinuxBridgeInNode(labName, nodeName, ports); err != nil {
					return fmt.Errorf("deploy/bridge %s: setup br0: %w", nodeName, err)
				}
			}

			// Sysctl per node; routers get net.ipv4.ip_forward=1 unless YAML overrides.
			for nodeName, n := range cfg.Topology.Nodes {
				if err := netns.ConfigureNodeSysctl(labName, nodeName, n); err != nil {
					return fmt.Errorf("deploy/sysctl %s: %w", nodeName, err)
				}
			}

			if err := mgmt.SetupMgmtBridge(cfg); err != nil {
				return fmt.Errorf("deploy/mgmt: %w", err)
			}

			routeStart := time.Now()
			if cfg.Routing.AutoStatic {
				rs, err := routing.ComputeStaticRoutes(cfg)
				if err != nil {
					return fmt.Errorf("deploy/route compute static: %w", err)
				}
				if err := routing.ApplyRoutes(cfg, rs); err != nil {
					return fmt.Errorf("deploy/route apply static: %w", err)
				}
			}

			// Hosts: default via local segment router (bridge or direct link). Routers unchanged.
			for hostName, gw := range topology.HostDefaultGateways(cfg) {
				if err := netns.AddDefaultRoute(labName, hostName, gw); err != nil {
					return fmt.Errorf("deploy/route host %s default via %s: %w", hostName, gw, err)
				}
			}
			logDeployStage("route", routeStart)

			// Link impairment (tc netem) on both endpoints.
			for li, link := range cfg.Topology.Links {
				if link.Netem == nil || !link.Netem.NetemActive() {
					continue
				}
				n0, i0 := config.SplitEndpointPublic(link.Endpoints[0])
				n1, i1 := config.SplitEndpointPublic(link.Endpoints[1])
				if err := netns.ApplyNetem(labName, n0, i0, link.Netem); err != nil {
					return fmt.Errorf("topology.links[%d] netem on %s:%s: %w", li, n0, i0, err)
				}
				if err := netns.ApplyNetem(labName, n1, i1, link.Netem); err != nil {
					return fmt.Errorf("topology.links[%d] netem on %s:%s: %w", li, n1, i1, err)
				}
			}

			nodeNames := make([]string, 0, len(cfg.Topology.Nodes))
			for n := range cfg.Topology.Nodes {
				nodeNames = append(nodeNames, n)
			}
			sort.Strings(nodeNames)
			for _, nodeName := range nodeNames {
				n := cfg.Topology.Nodes[nodeName]
				if err := netns.WriteNodeEnvFile(labName, nodeName, n.Env); err != nil {
					return fmt.Errorf("node %q env: %w", nodeName, err)
				}
			}
			startupExecStart := time.Now()
			for _, nodeName := range nodeNames {
				n := cfg.Topology.Nodes[nodeName]
				if err := netns.RunStartupExec(labName, nodeName, n.Exec, n.Env); err != nil {
					return fmt.Errorf("deploy/startup exec %s: %w", nodeName, err)
				}
			}
			logDeployStage("startup exec", startupExecStart)

			st := labstate.FromConfig(cfg)
			if err := labstate.Save(netns.LabStatePath(labName), st); err != nil {
				return fmt.Errorf("write lab state: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "YAML topology file")

	return cmd
}

func NewDestroyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy <lab>",
		Short: "Destroy a previously deployed lab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug := viper.GetBool("debug")
			if err := logx.Init(debug); err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			labName := args[0]
			st, err := labstate.Load(netns.LabStatePath(labName))
			if err != nil {
				return fmt.Errorf("load lab state for %q: %w", labName, err)
			}

			// Clear tc netem before tearing down netns.
			for _, link := range st.Links {
				if link.Netem == nil || !link.Netem.NetemActive() {
					continue
				}
				n0, i0 := config.SplitEndpointPublic(link.Endpoints[0])
				n1, i1 := config.SplitEndpointPublic(link.Endpoints[1])
				netns.ClearNetem(labName, n0, i0)
				netns.ClearNetem(labName, n1, i1)
			}

			// Remove management network resources if they were created.
			nodeNames := make([]string, 0, len(st.Nodes))
			for nodeName := range st.Nodes {
				nodeNames = append(nodeNames, nodeName)
			}
			if err := mgmt.TeardownMgmtByLab(labName, st.Mgmt.Enable, nodeNames); err != nil {
				return err
			}

			for nodeName := range st.Nodes {
				_ = netns.DeleteNamespace(labName, nodeName)
			}

			if err := netns.RemoveLabDirs(labName); err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}
