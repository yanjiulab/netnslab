package ui

import (
  "archive/tar"
  "compress/gzip"
  "crypto/rand"
  "encoding/hex"
  "fmt"
  "io"
  "os"
  "path/filepath"
  "sort"
  "strings"
  "sync"
  "time"

  "github.com/yanjiulab/netnslab/internal/labstate"
  "github.com/yanjiulab/netnslab/internal/netns"
)

type batchExportRequest struct {
  Nodes []string `json:"nodes"`
  Scope string   `json:"scope"` // selected|all
  Kinds []string `json:"kinds"` // pcap|state|logs
}

type batchExportResponse struct {
  DownloadURL string `json:"download_url"`
  Filename    string `json:"filename"`
  FileCount   int    `json:"file_count"`
  SizeBytes   int64  `json:"size_bytes"`
}

type exportArtifact struct {
  absPath string
  relPath string
}

type exportFileEntry struct {
  absPath   string
  filename  string
  expiresAt time.Time
}

var (
  exportMu    sync.Mutex
  exportFiles = map[string]exportFileEntry{}
)

func runBatchExport(labName string, req batchExportRequest) (batchExportResponse, error) {
  kinds := normalizeExportKinds(req.Kinds)
  if len(kinds) == 0 {
    return batchExportResponse{}, fmt.Errorf("kinds is required")
  }

  nodes, err := resolveExportNodes(labName, req.Scope, req.Nodes)
  if err != nil {
    return batchExportResponse{}, err
  }

  artifacts, err := collectArtifacts(labName, nodes, kinds)
  if err != nil {
    return batchExportResponse{}, err
  }
  if len(artifacts) == 0 {
    return batchExportResponse{}, fmt.Errorf("no artifacts found")
  }

  ts := time.Now().Format("20060102-150405")
  filename := fmt.Sprintf("netnslab-%s-artifacts-%s.tar.gz", labName, ts)
  outPath := filepath.Join(os.TempDir(), filename)
  size, err := writeTarGz(outPath, artifacts)
  if err != nil {
    return batchExportResponse{}, err
  }

  token, err := newExportToken()
  if err != nil {
    return batchExportResponse{}, err
  }
  exportMu.Lock()
  cleanupExpiredExportFilesLocked()
  exportFiles[token] = exportFileEntry{
    absPath:   outPath,
    filename:  filename,
    expiresAt: time.Now().Add(10 * time.Minute),
  }
  exportMu.Unlock()

  return batchExportResponse{
    DownloadURL: "/api/downloads/" + token,
    Filename:    filename,
    FileCount:   len(artifacts),
    SizeBytes:   size,
  }, nil
}

func downloadExportByToken(token string) (exportFileEntry, bool) {
  exportMu.Lock()
  defer exportMu.Unlock()
  cleanupExpiredExportFilesLocked()
  e, ok := exportFiles[token]
  if !ok {
    return exportFileEntry{}, false
  }
  return e, true
}

func removeExportToken(token string) {
  exportMu.Lock()
  e, ok := exportFiles[token]
  if ok {
    delete(exportFiles, token)
  }
  exportMu.Unlock()
  if ok {
    _ = os.Remove(e.absPath)
  }
}

func cleanupExpiredExportFilesLocked() {
  now := time.Now()
  for tk, e := range exportFiles {
    if now.After(e.expiresAt) {
      delete(exportFiles, tk)
      _ = os.Remove(e.absPath)
    }
  }
}

func resolveExportNodes(labName, scope string, reqNodes []string) ([]string, error) {
  st, err := labstate.Load(netns.LabStatePath(labName))
  if err != nil {
    return nil, fmt.Errorf("load lab state: %w", err)
  }
  all := make([]string, 0, len(st.Nodes))
  for n := range st.Nodes {
    all = append(all, n)
  }
  sort.Strings(all)

  if strings.TrimSpace(scope) == "" || scope == "all" {
    return all, nil
  }
  if scope != "selected" {
    return nil, fmt.Errorf("invalid scope")
  }
  if len(reqNodes) == 0 {
    return nil, fmt.Errorf("nodes is required for selected scope")
  }
  seen := map[string]struct{}{}
  out := make([]string, 0, len(reqNodes))
  for _, raw := range reqNodes {
    n := strings.TrimSpace(raw)
    if n == "" {
      continue
    }
    if _, ok := seen[n]; ok {
      continue
    }
    if _, ok := st.Nodes[n]; !ok {
      return nil, fmt.Errorf("node %q not found", n)
    }
    seen[n] = struct{}{}
    out = append(out, n)
  }
  if len(out) == 0 {
    return nil, fmt.Errorf("no valid selected nodes")
  }
  sort.Strings(out)
  return out, nil
}

func normalizeExportKinds(kinds []string) []string {
  allowed := map[string]struct{}{"pcap": {}, "state": {}, "logs": {}}
  seen := map[string]struct{}{}
  out := make([]string, 0, len(kinds))
  for _, k := range kinds {
    v := strings.ToLower(strings.TrimSpace(k))
    if _, ok := allowed[v]; !ok {
      continue
    }
    if _, ok := seen[v]; ok {
      continue
    }
    seen[v] = struct{}{}
    out = append(out, v)
  }
  sort.Strings(out)
  return out
}

func collectArtifacts(labName string, nodes []string, kinds []string) ([]exportArtifact, error) {
  kindSet := map[string]bool{}
  for _, k := range kinds {
    kindSet[k] = true
  }
  artifacts := make([]exportArtifact, 0)

  if kindSet["state"] {
    p := netns.LabStatePath(labName)
    if fileExists(p) {
      artifacts = append(artifacts, exportArtifact{
        absPath: p,
        relPath: filepath.Join(labName, "lab-state.json"),
      })
    }
  }

  for _, node := range nodes {
    if kindSet["pcap"] {
      dir := netns.RunDir(labName, node)
      gl, _ := filepath.Glob(filepath.Join(dir, "*.pcap"))
      sort.Strings(gl)
      for _, p := range gl {
        if !fileExists(p) {
          continue
        }
        artifacts = append(artifacts, exportArtifact{
          absPath: p,
          relPath: filepath.Join(labName, node, filepath.Base(p)),
        })
      }
    }
    if kindSet["logs"] {
      p := netns.LogFile(labName, node)
      if fileExists(p) {
        artifacts = append(artifacts, exportArtifact{
          absPath: p,
          relPath: filepath.Join(labName, "logs", filepath.Base(p)),
        })
      }
    }
  }
  return artifacts, nil
}

func writeTarGz(outPath string, artifacts []exportArtifact) (int64, error) {
  f, err := os.Create(outPath)
  if err != nil {
    return 0, fmt.Errorf("create export file: %w", err)
  }
  defer func() { _ = f.Close() }()

  gz := gzip.NewWriter(f)
  tw := tar.NewWriter(gz)

  for _, a := range artifacts {
    info, err := os.Stat(a.absPath)
    if err != nil || info.IsDir() {
      continue
    }
    hdr, err := tar.FileInfoHeader(info, "")
    if err != nil {
      continue
    }
    hdr.Name = filepath.ToSlash(a.relPath)
    if err := tw.WriteHeader(hdr); err != nil {
      continue
    }
    in, err := os.Open(a.absPath)
    if err != nil {
      continue
    }
    _, _ = io.Copy(tw, in)
    _ = in.Close()
  }
  if err := tw.Close(); err != nil {
    return 0, fmt.Errorf("close tar: %w", err)
  }
  if err := gz.Close(); err != nil {
    return 0, fmt.Errorf("close gzip: %w", err)
  }
  st, err := os.Stat(outPath)
  if err != nil {
    return 0, err
  }
  return st.Size(), nil
}

func newExportToken() (string, error) {
  b := make([]byte, 16)
  if _, err := rand.Read(b); err != nil {
    return "", err
  }
  return hex.EncodeToString(b), nil
}

func fileExists(path string) bool {
  st, err := os.Stat(path)
  return err == nil && !st.IsDir()
}
