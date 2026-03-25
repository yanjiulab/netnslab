package netns

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const nodeEnvJSON = "node-env.json"

// NodeEnvFilePath is the per-node persisted environment (for exec/enter and deploy-time scripts).
func NodeEnvFilePath(labName, nodeName string) string {
	return filepath.Join(RunDir(labName, nodeName), nodeEnvJSON)
}

// WriteNodeEnvFile writes node env to disk, or removes the file when empty.
func WriteNodeEnvFile(labName, nodeName string, env map[string]string) error {
	path := NodeEnvFilePath(labName, nodeName)
	if len(env) == 0 {
		_ = os.Remove(path)
		return nil
	}
	clean := make(map[string]string)
	for k, v := range env {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		clean[k] = v
	}
	if len(clean) == 0 {
		_ = os.Remove(path)
		return nil
	}
	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal node env: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// ReadNodeEnvFile loads persisted node env; missing file yields nil map and nil error.
func ReadNodeEnvFile(labName, nodeName string) (map[string]string, error) {
	path := NodeEnvFilePath(labName, nodeName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

// MergeEnviron returns base environment with overrides applied (last wins per key).
func MergeEnviron(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	skip := make(map[string]struct{})
	for k := range overrides {
		k = strings.TrimSpace(k)
		if k != "" {
			skip[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, e := range base {
		i := strings.IndexByte(e, '=')
		if i <= 0 {
			out = append(out, e)
			continue
		}
		k := e[:i]
		if _, ok := skip[k]; ok {
			continue
		}
		out = append(out, e)
	}
	for k, v := range overrides {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out = append(out, k+"="+v)
	}
	return out
}
