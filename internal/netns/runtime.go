package netns

import "fmt"

// CreateNamespace creates a named network namespace for a lab node.
func CreateNamespace(labName, nodeName string) error {
	nsName := NamespaceName(labName, nodeName)
	if err := runIP("netns", "add", nsName); err != nil {
		return fmt.Errorf("create netns %s: %w", nsName, err)
	}
	return nil
}

// DeleteNamespace deletes the network namespace for a lab node.
func DeleteNamespace(labName, nodeName string) error {
	nsName := NamespaceName(labName, nodeName)
	if err := runIP("netns", "del", nsName); err != nil {
		return fmt.Errorf("delete netns %s: %w", nsName, err)
	}
	return nil
}

// CreateVethPair creates a veth pair on the host and moves each end into the
// corresponding node namespace with the requested interface names.
func CreateVethPair(labName, nodeA, ifA, nodeB, ifB string) error {
	nsA := NamespaceName(labName, nodeA)
	nsB := NamespaceName(labName, nodeB)

	hostIfA := hostInterfaceName(nodeA, ifA)
	hostIfB := hostInterfaceName(nodeB, ifB)

	if err := runIP("link", "add", hostIfA, "type", "veth", "peer", "name", hostIfB); err != nil {
		return err
	}

	if err := runIP("link", "set", hostIfA, "netns", nsA); err != nil {
		return err
	}
	if err := runIP("link", "set", hostIfB, "netns", nsB); err != nil {
		return err
	}

	if err := runInNetns(nsA, "ip", "link", "set", hostIfA, "name", ifA); err != nil {
		return err
	}
	if err := runInNetns(nsB, "ip", "link", "set", hostIfB, "name", ifB); err != nil {
		return err
	}

	return nil
}

// CreateBridge creates a Linux bridge on the host.
func CreateBridge(name string) error {
	if err := runIP("link", "add", name, "type", "bridge"); err != nil {
		return err
	}
	if err := runIP("link", "set", name, "up"); err != nil {
		return err
	}
	return nil
}


func hostInterfaceName(nodeName, ifName string) string {
	return fmt.Sprintf("v-%s-%s", nodeName, ifName)
}

// DeleteLink deletes a link (veth, bridge, etc.) on the host by name.
func DeleteLink(name string) error {
	if err := runIP("link", "del", name); err != nil {
		return err
	}
	return nil
}
