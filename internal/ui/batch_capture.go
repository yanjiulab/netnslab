package ui

import (
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "sort"
  "strconv"
  "strings"
  "sync"
  "time"

  "github.com/yanjiulab/netnslab/internal/labstate"
  "github.com/yanjiulab/netnslab/internal/netns"
)

type batchCaptureStartRequest struct {
  Nodes       []string `json:"nodes"`
  IfName      string   `json:"ifname"`
  Filter      string   `json:"filter"`
  PacketCount int      `json:"packet_count"`
}

type batchCaptureStopRequest struct {
  Nodes []string `json:"nodes"`
}

type batchCaptureResult struct {
  Node     string `json:"node"`
  Status   string `json:"status"`
  Message  string `json:"message,omitempty"`
  PcapPath string `json:"pcap_path,omitempty"`
}

type batchCaptureResponse struct {
  Results []batchCaptureResult `json:"results"`
}

type captureTask struct {
  lab      string
  node     string
  ifName   string
  pcapPath string
  cmd      *exec.Cmd
  started  time.Time
}

var (
  captureMu    sync.Mutex
  captureTasks = map[string]*captureTask{}
)

func captureTaskKey(lab, node string) string {
  return lab + "|" + node
}

func runBatchCaptureStart(labName string, req batchCaptureStartRequest) (batchCaptureResponse, error) {
  st, nodes, err := validateBatchNodes(labName, req.Nodes)
  if err != nil {
    return batchCaptureResponse{}, err
  }
  _ = st

  ifName := strings.TrimSpace(req.IfName)
  if ifName == "" {
    ifName = "any"
  }
  filter := strings.TrimSpace(req.Filter)
  packetCount := req.PacketCount
  if packetCount < 0 {
    packetCount = 0
  }

  out := make([]batchCaptureResult, 0, len(nodes))
  for _, node := range nodes {
    res := startCaptureForNode(labName, node, ifName, filter, packetCount)
    out = append(out, res)
  }
  sort.SliceStable(out, func(i, j int) bool { return out[i].Node < out[j].Node })
  return batchCaptureResponse{Results: out}, nil
}

func runBatchCaptureStop(labName string, req batchCaptureStopRequest) (batchCaptureResponse, error) {
  _, nodes, err := validateBatchNodes(labName, req.Nodes)
  if err != nil {
    return batchCaptureResponse{}, err
  }
  out := make([]batchCaptureResult, 0, len(nodes))
  for _, node := range nodes {
    out = append(out, stopCaptureForNode(labName, node))
  }
  sort.SliceStable(out, func(i, j int) bool { return out[i].Node < out[j].Node })
  return batchCaptureResponse{Results: out}, nil
}

func listBatchCaptureTasks(labName string) batchCaptureResponse {
  captureMu.Lock()
  defer captureMu.Unlock()
  out := make([]batchCaptureResult, 0)
  for _, t := range captureTasks {
    if t == nil || t.lab != labName {
      continue
    }
    out = append(out, batchCaptureResult{
      Node:     t.node,
      Status:   "running",
      Message:  fmt.Sprintf("if=%s started=%s", t.ifName, t.started.Format(time.RFC3339)),
      PcapPath: t.pcapPath,
    })
  }
  sort.SliceStable(out, func(i, j int) bool { return out[i].Node < out[j].Node })
  return batchCaptureResponse{Results: out}
}

func validateBatchNodes(labName string, reqNodes []string) (*labstate.Persisted, []string, error) {
  if len(reqNodes) == 0 {
    return nil, nil, fmt.Errorf("nodes is required")
  }
  st, err := labstate.Load(netns.LabStatePath(labName))
  if err != nil {
    return nil, nil, fmt.Errorf("load lab state: %w", err)
  }
  seen := make(map[string]struct{}, len(reqNodes))
  nodes := make([]string, 0, len(reqNodes))
  for _, n := range reqNodes {
    name := strings.TrimSpace(n)
    if name == "" {
      continue
    }
    if _, ok := seen[name]; ok {
      continue
    }
    if _, ok := st.Nodes[name]; !ok {
      return nil, nil, fmt.Errorf("node %q not found in lab %q", name, labName)
    }
    seen[name] = struct{}{}
    nodes = append(nodes, name)
  }
  if len(nodes) == 0 {
    return nil, nil, fmt.Errorf("no valid nodes selected")
  }
  return st, nodes, nil
}

func startCaptureForNode(labName, nodeName, ifName, filter string, packetCount int) batchCaptureResult {
  key := captureTaskKey(labName, nodeName)

  captureMu.Lock()
  if existing := captureTasks[key]; existing != nil {
    captureMu.Unlock()
    return batchCaptureResult{
      Node:     nodeName,
      Status:   "skipped",
      Message:  "capture already running",
      PcapPath: existing.pcapPath,
    }
  }
  captureMu.Unlock()

  nodeEnv, err := netns.ReadNodeEnvFile(labName, nodeName)
  if err != nil {
    return batchCaptureResult{Node: nodeName, Status: "error", Message: "read node env failed: " + err.Error()}
  }

  ts := time.Now().Format("20060102-150405")
  pcapName := fmt.Sprintf("capture-%s-%s-%s.pcap", nodeName, strings.ReplaceAll(ifName, "/", "_"), ts)
  pcapPath := filepath.Join(netns.RunDir(labName, nodeName), pcapName)
  nsName := netns.NamespaceName(labName, nodeName)

  args := []string{"netns", "exec", nsName, "tcpdump", "-U", "-n", "-i", ifName, "-w", pcapPath}
  if packetCount > 0 {
    args = append(args, "-c", strconv.Itoa(packetCount))
  }
  if filter != "" {
    args = append(args, strings.Fields(filter)...)
  }

  cmd := exec.Command("ip", args...)
  cmd.Env = netns.MergeEnviron(os.Environ(), nodeEnv)

  if err := cmd.Start(); err != nil {
    return batchCaptureResult{Node: nodeName, Status: "error", Message: "start tcpdump failed: " + err.Error()}
  }

  task := &captureTask{
    lab:      labName,
    node:     nodeName,
    ifName:   ifName,
    pcapPath: pcapPath,
    cmd:      cmd,
    started:  time.Now(),
  }
  captureMu.Lock()
  captureTasks[key] = task
  captureMu.Unlock()

  go func(k string, t *captureTask) {
    _ = t.cmd.Wait()
    captureMu.Lock()
    if cur := captureTasks[k]; cur == t {
      delete(captureTasks, k)
    }
    captureMu.Unlock()
  }(key, task)

  return batchCaptureResult{
    Node:     nodeName,
    Status:   "started",
    PcapPath: pcapPath,
  }
}

func stopCaptureForNode(labName, nodeName string) batchCaptureResult {
  key := captureTaskKey(labName, nodeName)
  captureMu.Lock()
  task := captureTasks[key]
  if task != nil {
    // Remove from in-memory running list immediately to avoid UI lag/race.
    delete(captureTasks, key)
  }
  captureMu.Unlock()
  if task == nil {
    return batchCaptureResult{Node: nodeName, Status: "not_running", Message: "no running capture"}
  }
  if task.cmd == nil || task.cmd.Process == nil {
    return batchCaptureResult{Node: nodeName, Status: "error", Message: "invalid capture process", PcapPath: task.pcapPath}
  }
  if err := task.cmd.Process.Signal(os.Interrupt); err != nil {
    if errKill := task.cmd.Process.Kill(); errKill != nil {
      return batchCaptureResult{Node: nodeName, Status: "error", Message: "stop capture failed: " + errKill.Error(), PcapPath: task.pcapPath}
    }
  }
  return batchCaptureResult{Node: nodeName, Status: "stopped", PcapPath: task.pcapPath}
}
