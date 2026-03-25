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
	Routes  []string  `json:"routes,omitempty"`
}

type uiIface struct {
	IfName    string `json:"ifname"`
	IPv4      string `json:"ipv4"`
	Up        bool   `json:"up"`
	OperState string `json:"operstate,omitempty"`
	TcQdisc   string `json:"tc,omitempty"`
}

type uiLinkEnd struct {
	Node      string `json:"node"`
	IfName    string `json:"ifname"`
	IPv4      string `json:"ipv4"`
	Up        bool   `json:"up"`
	OperState string `json:"operstate,omitempty"`
	TcQdisc   string `json:"tc,omitempty"`
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
		live := isTruthy(r.URL.Query().Get("live"))
		selectedNode := strings.TrimSpace(r.URL.Query().Get("node"))
		topo, err := buildTopology(labName, live, selectedNode)
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

	// Interactive node terminal (websocket + PTY).
	mux.HandleFunc("/ws/labs/", terminalWSHandler(labFilter))

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

func buildTopology(labName string, live bool, selectedNode string) (*uiTopology, error) {
	st, err := labstate.Load(netns.LabStatePath(labName))
	if err != nil {
		return nil, err
	}

	needSelectedLive := live && selectedNode != ""

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

	// mgmt eth0 should be visible even if not referenced by topology.links.
	if st.Mgmt.Enable {
		for nodeName := range st.Nodes {
			ifacesByNode[nodeName] = ensureIfaceSet(ifacesByNode[nodeName], netns.MgmtIfaceName)
		}
	}

	nodes := make([]uiNode, 0, len(st.Nodes))
	nodeNames := make([]string, 0, len(st.Nodes))
	for n := range st.Nodes {
		nodeNames = append(nodeNames, n)
	}
	sort.Strings(nodeNames)

	// Cache interface queries to avoid duplicated ip/netns exec calls.
	ipByNodeIf := make(map[string]map[string]string)
	upByNodeIf := make(map[string]map[string]bool)
	operByNodeIf := make(map[string]map[string]string)
	tcByNodeIf := make(map[string]map[string]string) // only for selectedNode

	for _, nodeName := range nodeNames {
		if ipByNodeIf[nodeName] == nil {
			ipByNodeIf[nodeName] = make(map[string]string)
		}
		if live {
			if upByNodeIf[nodeName] == nil {
				upByNodeIf[nodeName] = make(map[string]bool)
			}
			if operByNodeIf[nodeName] == nil {
				operByNodeIf[nodeName] = make(map[string]string)
			}
		}
		if needSelectedLive && nodeName == selectedNode {
			tcByNodeIf[nodeName] = make(map[string]string)
		}

		for ifName := range ifacesByNode[nodeName] {
			ipByNodeIf[nodeName][ifName] = netns.QueryIfaceIPv4(labName, nodeName, ifName)
			if live {
				up, oper := netns.QueryIfaceUp(labName, nodeName, ifName)
				upByNodeIf[nodeName][ifName] = up
				operByNodeIf[nodeName][ifName] = oper
				if needSelectedLive && nodeName == selectedNode {
					tcByNodeIf[nodeName][ifName] = netns.QueryTcQdisc(labName, nodeName, ifName)
				}
			}
		}
	}

	var routesSelected []string
	if needSelectedLive {
		routesSelected = netns.QueryRoutes(labName, selectedNode)
	}

	for _, nodeName := range nodeNames {
		kind := st.Nodes[nodeName]
		var ifaces []uiIface

		for ifName := range ifacesByNode[nodeName] {
			ip := ipByNodeIf[nodeName][ifName]
			ui := uiIface{
				IfName: ifName,
				IPv4:   ip,
				Up:     false,
			}
			if live {
				ui.Up = upByNodeIf[nodeName][ifName]
				ui.OperState = operByNodeIf[nodeName][ifName]
				if needSelectedLive && nodeName == selectedNode {
					ui.TcQdisc = tcByNodeIf[nodeName][ifName]
				}
			}
			ifaces = append(ifaces, ui)
		}

		sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].IfName < ifaces[j].IfName })

		hasMgmt := st.Mgmt.Enable
		mgmtIP := ""
		if hasMgmt {
			mgmtIP = ipByNodeIf[nodeName][netns.MgmtIfaceName]
		}

		nodes = append(nodes, uiNode{
			Name:    nodeName,
			Kind:    kind,
			MgmtIP:  mgmtIP,
			Ifaces:  ifaces,
			HasMgmt: hasMgmt,
			Routes: func() []string {
				if needSelectedLive && nodeName == selectedNode {
					return routesSelected
				}
				return nil
			}(),
		})
	}

	links := make([]uiLink, 0, len(st.Links))
	for _, l := range st.Links {
		aNode, aIf := config.SplitEndpointPublic(l.Endpoints[0])
		bNode, bIf := config.SplitEndpointPublic(l.Endpoints[1])

		aIP := ipByNodeIf[aNode][aIf]
		bIP := ipByNodeIf[bNode][bIf]

		netemSummary := "-"
		if l.Netem != nil && l.Netem.NetemActive() {
			netemSummary = l.Netem.NetemSummary()
		}
		aUp, bUp := false, false
		aOper, bOper := "", ""
		aTc, bTc := "", ""
		if live {
			aUp = upByNodeIf[aNode][aIf]
			bUp = upByNodeIf[bNode][bIf]
			aOper = operByNodeIf[aNode][aIf]
			bOper = operByNodeIf[bNode][bIf]
			if needSelectedLive && aNode == selectedNode {
				aTc = tcByNodeIf[aNode][aIf]
			}
			if needSelectedLive && bNode == selectedNode {
				bTc = tcByNodeIf[bNode][bIf]
			}
		}
		links = append(links, uiLink{
			A: uiLinkEnd{
				Node:      aNode,
				IfName:    aIf,
				IPv4:      aIP,
				Up:        aUp,
				OperState: aOper,
				TcQdisc:   aTc,
			},
			B: uiLinkEnd{
				Node:      bNode,
				IfName:    bIf,
				IPv4:      bIP,
				Up:        bUp,
				OperState: bOper,
				TcQdisc:   bTc,
			},
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

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func ensureIfaceSet(set map[string]struct{}, ifName string) map[string]struct{} {
	if set == nil {
		set = make(map[string]struct{})
	}
	set[ifName] = struct{}{}
	return set
}
