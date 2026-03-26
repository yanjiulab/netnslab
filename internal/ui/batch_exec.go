package ui

import (
  "bytes"
  "context"
  "errors"
  "fmt"
  "os"
  "os/exec"
  "sort"
  "strings"
  "sync"
  "time"

  "github.com/yanjiulab/netnslab/internal/labstate"
  "github.com/yanjiulab/netnslab/internal/netns"
)

type batchExecRequest struct {
  Nodes       []string `json:"nodes"`
  Command     string   `json:"command"`
  TimeoutSec  int      `json:"timeout_sec"`
  Parallelism int      `json:"parallelism"`
}

type batchExecNodeResult struct {
  Node       string `json:"node"`
  ExitCode   int    `json:"exit_code"`
  StdoutTail string `json:"stdout_tail"`
  StderrTail string `json:"stderr_tail"`
  DurationMs int64  `json:"duration_ms"`
}

type batchExecResponse struct {
  Results []batchExecNodeResult `json:"results"`
}

func runBatchExec(labName string, req batchExecRequest) (batchExecResponse, error) {
  cmdText := strings.TrimSpace(req.Command)
  if cmdText == "" {
    return batchExecResponse{}, fmt.Errorf("command is required")
  }
  if len(req.Nodes) == 0 {
    return batchExecResponse{}, fmt.Errorf("nodes is required")
  }

  st, err := labstate.Load(netns.LabStatePath(labName))
  if err != nil {
    return batchExecResponse{}, fmt.Errorf("load lab state: %w", err)
  }

  seen := make(map[string]struct{}, len(req.Nodes))
  nodes := make([]string, 0, len(req.Nodes))
  for _, n := range req.Nodes {
    name := strings.TrimSpace(n)
    if name == "" {
      continue
    }
    if _, ok := seen[name]; ok {
      continue
    }
    if _, ok := st.Nodes[name]; !ok {
      return batchExecResponse{}, fmt.Errorf("node %q not found in lab %q", name, labName)
    }
    seen[name] = struct{}{}
    nodes = append(nodes, name)
  }
  if len(nodes) == 0 {
    return batchExecResponse{}, fmt.Errorf("no valid nodes selected")
  }

  timeoutSec := req.TimeoutSec
  if timeoutSec <= 0 {
    timeoutSec = 10
  }
  if timeoutSec > 300 {
    timeoutSec = 300
  }

  parallelism := req.Parallelism
  if parallelism <= 0 {
    parallelism = 4
  }
  if parallelism > 32 {
    parallelism = 32
  }
  if parallelism > len(nodes) {
    parallelism = len(nodes)
  }

  results := make([]batchExecNodeResult, len(nodes))
  type job struct {
    idx  int
    node string
  }
  ch := make(chan job, len(nodes))
  for i, n := range nodes {
    ch <- job{idx: i, node: n}
  }
  close(ch)

  var wg sync.WaitGroup
  for i := 0; i < parallelism; i++ {
    wg.Add(1)
    go func() {
      defer wg.Done()
      for j := range ch {
        results[j.idx] = runBatchExecSingle(labName, j.node, cmdText, timeoutSec)
      }
    }()
  }
  wg.Wait()

  sort.SliceStable(results, func(i, j int) bool { return results[i].Node < results[j].Node })
  return batchExecResponse{Results: results}, nil
}

func runBatchExecSingle(labName, nodeName, cmdText string, timeoutSec int) batchExecNodeResult {
  start := time.Now()
  res := batchExecNodeResult{
    Node:     nodeName,
    ExitCode: 0,
  }

  nodeEnv, err := netns.ReadNodeEnvFile(labName, nodeName)
  if err != nil {
    res.ExitCode = -1
    res.StderrTail = tailText("read node env failed: "+err.Error(), 2000)
    res.DurationMs = time.Since(start).Milliseconds()
    return res
  }

  nsName := netns.NamespaceName(labName, nodeName)
  ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
  defer cancel()

  c := exec.CommandContext(ctx, "ip", "netns", "exec", nsName, "bash", "-lc", cmdText)
  c.Env = netns.MergeEnviron(os.Environ(), nodeEnv)

  var outBuf, errBuf bytes.Buffer
  c.Stdout = &outBuf
  c.Stderr = &errBuf

  runErr := c.Run()
  if runErr != nil {
    res.ExitCode = -1
    var ex *exec.ExitError
    if errors.As(runErr, &ex) {
      res.ExitCode = ex.ExitCode()
    }
    if errors.Is(ctx.Err(), context.DeadlineExceeded) {
      res.ExitCode = 124
      if errBuf.Len() > 0 {
        _, _ = errBuf.WriteString("\n")
      }
      _, _ = errBuf.WriteString(fmt.Sprintf("timeout after %ds", timeoutSec))
    }
  }

  res.StdoutTail = tailText(outBuf.String(), 4000)
  res.StderrTail = tailText(errBuf.String(), 4000)
  res.DurationMs = time.Since(start).Milliseconds()
  return res
}

func tailText(s string, max int) string {
  if max <= 0 || len(s) <= max {
    return strings.TrimSpace(s)
  }
  return strings.TrimSpace("...(truncated)\n" + s[len(s)-max:])
}
