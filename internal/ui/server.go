package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/yanjiulab/netnslab/internal/config"
	"github.com/yanjiulab/netnslab/internal/labstate"
	"github.com/yanjiulab/netnslab/internal/logx"
	"github.com/yanjiulab/netnslab/internal/netns"
)

type labsIndex struct {
	Name string `json:"name"`
}

type uiNode struct {
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	MgmtIP  string    `json:"mgmt_ip"`
	Ifaces  []uiIface `json:"ifaces,omitempty"`
	HasMgmt bool      `json:"has_mgmt"`
}

type uiIface struct {
	IfName string `json:"ifname"`
	IPv4   string `json:"ipv4"`
}

type uiLinkEnd struct {
	Node   string `json:"node"`
	IfName string `json:"ifname"`
	IPv4   string `json:"ipv4"`
}

type uiLink struct {
	A     uiLinkEnd `json:"a"`
	B     uiLinkEnd `json:"b"`
	Netem string    `json:"netem"`
}

type uiTopology struct {
	Lab       string   `json:"lab"`
	UpdatedAt int64    `json:"updated_at"`
	Nodes     []uiNode `json:"nodes"`
	Links     []uiLink `json:"links"`
}

func Serve(addr, labFilter string) error {
	mux := http.NewServeMux()

	// Static assets.
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("ui: sub staticFS: %w", err)
	}
	static := http.StripPrefix("/ui/", http.FileServer(http.FS(sub)))
	mux.Handle("/ui/", static)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/index.html", http.StatusFound)
	})

	// API.
	mux.HandleFunc("/api/labs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		type resp struct {
			Labs []labsIndex `json:"labs"`
		}
		labs, err := listLabs()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if labFilter != "" {
			labs = filterLabs(labs, labFilter)
		}
		out := resp{Labs: labs}
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/api/labs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Expected:
		//   /api/labs/{lab}/topology
		//   /api/labs/{lab}/...
		u, err := url.Parse(r.URL.Path)
		if err != nil {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		p := strings.TrimPrefix(u.Path, "/api/labs/")
		p = strings.TrimSuffix(p, "/")
		parts := strings.Split(p, "/")
		if len(parts) != 2 || parts[1] != "topology" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		labName := parts[0]
		if labFilter != "" && labName != labFilter {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		topo, err := buildTopology(labName)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, fmt.Sprintf("lab %q not deployed", labName), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(topo)
	})

	logx.S().Infof("ui listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func listLabs() ([]labsIndex, error) {
	entries, err := os.ReadDir(netns.RunBaseDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []labsIndex{}, nil
		}
		return nil, err
	}

	type item struct {
		name string
	}
	var out []labsIndex
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		labName := e.Name()
		if _, err := os.Stat(netns.LabStatePath(labName)); err != nil {
			continue // only show deployed labs
		}
		out = append(out, labsIndex{Name: labName})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func filterLabs(in []labsIndex, want string) []labsIndex {
	var out []labsIndex
	for _, l := range in {
		if l.Name == want {
			out = append(out, l)
		}
	}
	return out
}

func buildTopology(labName string) (*uiTopology, error) {
	st, err := labstate.Load(netns.LabStatePath(labName))
	if err != nil {
		return nil, err
	}

	// Collect interface names from link endpoints.
	ifacesByNode := make(map[string]map[string]struct{})
	for _, l := range st.Links {
		aNode, aIf := config.SplitEndpointPublic(l.Endpoints[0])
		bNode, bIf := config.SplitEndpointPublic(l.Endpoints[1])
		if aNode != "" && aIf != "" {
			ifacesByNode[aNode] = ensureIfaceSet(ifacesByNode[aNode], aIf)
		}
		if bNode != "" && bIf != "" {
			ifacesByNode[bNode] = ensureIfaceSet(ifacesByNode[bNode], bIf)
		}
	}

	nodes := make([]uiNode, 0, len(st.Nodes))
	nodeNames := make([]string, 0, len(st.Nodes))
	for n := range st.Nodes {
		nodeNames = append(nodeNames, n)
	}
	sort.Strings(nodeNames)

	for _, nodeName := range nodeNames {
		kind := st.Nodes[nodeName]
		var ifaces []uiIface
		for ifName := range ifacesByNode[nodeName] {
			ip := netns.QueryIfaceIPv4(labName, nodeName, ifName)
			ifaces = append(ifaces, uiIface{IfName: ifName, IPv4: ip})
		}
		// mgmt eth0 only if enabled; QueryIfaceIPv4 already returns empty on failure.
		mgmtIP := ""
		hasMgmt := st.Mgmt.Enable
		if hasMgmt {
			mgmtIP = netns.QueryIfaceIPv4(labName, nodeName, netns.MgmtIfaceName)
		}
		sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].IfName < ifaces[j].IfName })
		nodes = append(nodes, uiNode{
			Name:    nodeName,
			Kind:    kind,
			MgmtIP:  mgmtIP,
			Ifaces:  ifaces,
			HasMgmt: hasMgmt,
		})
	}

	links := make([]uiLink, 0, len(st.Links))
	for _, l := range st.Links {
		aNode, aIf := config.SplitEndpointPublic(l.Endpoints[0])
		bNode, bIf := config.SplitEndpointPublic(l.Endpoints[1])

		aIP := netns.QueryIfaceIPv4(labName, aNode, aIf)
		bIP := netns.QueryIfaceIPv4(labName, bNode, bIf)

		netemSummary := "-"
		if l.Netem != nil && l.Netem.NetemActive() {
			netemSummary = l.Netem.NetemSummary()
		}
		links = append(links, uiLink{
			A:     uiLinkEnd{Node: aNode, IfName: aIf, IPv4: aIP},
			B:     uiLinkEnd{Node: bNode, IfName: bIf, IPv4: bIP},
			Netem: netemSummary,
		})
	}

	return &uiTopology{
		Lab:       labName,
		UpdatedAt: time.Now().Unix(),
		Nodes:     nodes,
		Links:     links,
	}, nil
}

func ensureIfaceSet(set map[string]struct{}, ifName string) map[string]struct{} {
	if set == nil {
		set = make(map[string]struct{})
	}
	set[ifName] = struct{}{}
	return set
}
