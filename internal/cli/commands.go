package cli

import (
	"fmt"

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

			if _, err := topology.Build(cfg); err != nil {
				return err
			}
			if err := topology.AllocateAddresses(cfg); err != nil {
				return err
			}

			labName := cfg.Name

			for nodeName := range cfg.Topology.Nodes {
				if err := netns.EnsureLabDirs(labName, nodeName); err != nil {
					return err
				}
				if err := netns.CreateNamespace(labName, nodeName); err != nil {
					return err
				}
				if err := netns.EnsureLoopbackUp(labName, nodeName); err != nil {
					return err
				}
			}

			for _, link := range cfg.Topology.Links {
				nodeA, ifA := config.SplitEndpointPublic(link.Endpoints[0])
				nodeB, ifB := config.SplitEndpointPublic(link.Endpoints[1])

				if err := netns.CreateVethPair(labName, nodeA, ifA, nodeB, ifB); err != nil {
					return err
				}

				ifaceA := cfg.Topology.Nodes[nodeA].Interfaces[ifA]
				ifaceB := cfg.Topology.Nodes[nodeB].Interfaces[ifB]

				if err := netns.ConfigureLinkInterface(labName, nodeA, ifA, ifaceA.IP); err != nil {
					return err
				}
				if err := netns.ConfigureLinkInterface(labName, nodeB, ifB, ifaceB.IP); err != nil {
					return err
				}
			}

			// Linux bridge inside each kind=bridge netns: L2 between all bridge ports.
			for nodeName, n := range cfg.Topology.Nodes {
				if n.Kind != "bridge" {
					continue
				}
				ports := topology.BridgePortIfaces(cfg, nodeName)
				if err := netns.SetupLinuxBridgeInNode(labName, nodeName, ports); err != nil {
					return err
				}
			}

			// Sysctl per node; routers get net.ipv4.ip_forward=1 unless YAML overrides.
			for nodeName, n := range cfg.Topology.Nodes {
				if err := netns.ConfigureNodeSysctl(labName, nodeName, n); err != nil {
					return err
				}
			}

			if err := mgmt.SetupMgmtBridge(cfg); err != nil {
				return err
			}

			if cfg.Routing.AutoStatic {
				rs, err := routing.ComputeStaticRoutes(cfg)
				if err != nil {
					return err
				}
				if err := routing.ApplyRoutes(cfg, rs); err != nil {
					return err
				}
			}

			// Hosts: default via local segment router (bridge or direct link). Routers unchanged.
			for hostName, gw := range topology.HostDefaultGateways(cfg) {
				if err := netns.AddDefaultRoute(labName, hostName, gw); err != nil {
					return fmt.Errorf("default route for host %s: %w", hostName, err)
				}
			}

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
	var file string

	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy a previously deployed lab",
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

			// Clear tc netem before tearing down netns.
			for _, link := range cfg.Topology.Links {
				if link.Netem == nil || !link.Netem.NetemActive() {
					continue
				}
				n0, i0 := config.SplitEndpointPublic(link.Endpoints[0])
				n1, i1 := config.SplitEndpointPublic(link.Endpoints[1])
				netns.ClearNetem(labName, n0, i0)
				netns.ClearNetem(labName, n1, i1)
			}

			// Remove management network resources if they were created.
			if err := mgmt.TeardownMgmtBridge(cfg); err != nil {
				return err
			}

			for nodeName := range cfg.Topology.Nodes {
				_ = netns.DeleteNamespace(labName, nodeName)
			}

			if err := netns.RemoveLabDirs(labName); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "YAML topology file")

	return cmd
}
