package netns

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	runBase = "/var/run/netnslab"
	logBase = "/var/log/netnslab"
)

// NamespaceName returns the OS-level namespace name for a lab node.
func NamespaceName(labName, nodeName string) string {
	return fmt.Sprintf("netnslab-%s-%s", labName, nodeName)
}

// RunDir returns the runtime directory for a specific lab/node.
func RunDir(labName, nodeName string) string {
	return filepath.Join(runBase, labName, nodeName)
}

// LogFile returns the log file path for a specific lab/node.
func LogFile(labName, nodeName string) string {
	return filepath.Join(logBase, labName, nodeName+".log")
}

// EnsureLabDirs creates runtime and log directories for the given lab/node.
func EnsureLabDirs(labName, nodeName string) error {
	if err := os.MkdirAll(RunDir(labName, nodeName), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(LogFile(labName, nodeName)), 0o755); err != nil {
		return err
	}
	return nil
}

// RemoveLabDirs removes runtime and log directories for the given lab.
// It is best-effort and ignores missing paths.
func RemoveLabDirs(labName string) error {
	_ = os.RemoveAll(filepath.Join(runBase, labName))
	_ = os.RemoveAll(filepath.Join(logBase, labName))
	return nil
}

// RunBaseDir exposes the base runtime directory for listing labs.
func RunBaseDir() string {
	return runBase
}

// LabRunDir is the per-lab directory under the run base (contains node subdirs and lab-state.json).
func LabRunDir(labName string) string {
	return filepath.Join(runBase, labName)
}

// LabStatePath returns the path to persisted lab metadata for show/list validation.
func LabStatePath(labName string) string {
	return filepath.Join(LabRunDir(labName), "lab-state.json")
}

