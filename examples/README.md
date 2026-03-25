## netnslab Examples

This directory contains example lab configurations for netnslab. The YAML schema
is summarized in the project [`README.md`](../README.md) (`name`, `routing`,
`addressing`, `mgmt`, `topology.nodes`, `topology.links`).

**Link `netem` (optional):** under a link, you may set `netem.delay_ms`, `netem.jitter_ms`,
and/or `netem.loss_percent`. Deploy runs `tc qdisc replace … netem` on **both** endpoints
(requires `sch_netem`). `netnslab show <lab>` prints a `netem=` summary per link from lab state.

### How to run an example

From the project root:

```bash
go build ./cmd/netnslab

# Deploy
sudo ./netnslab deploy -f examples/<file>.yaml --debug

# Enter a node
sudo ./netnslab enter <name_from_yaml> <node>

# Or execute a one-off command
sudo ./netnslab exec <name_from_yaml> <node> -- ip addr

# Show live state (after deploy; reads netns + lab-state.json)
sudo ./netnslab show <name_from_yaml>

# Graphviz DOT with live interface IPs (after deploy)
sudo ./netnslab graph <name_from_yaml> > /tmp/lab.dot

# E2E: h1 pings r1 on demo-lab (requires root)
sudo ./examples/demo-ping-test.sh

# Destroy
sudo ./netnslab destroy <name_from_yaml>
```

You must run netnslab as root (or via sudo), because it manipulates network
namespaces and interfaces.

### 1. `demo-lab.yaml`

- **Lab name**: `demo-lab`
- **Topology**:
  - 2 hosts: `h1`, `h2`
  - 2 routers: `r1`, `r2`
  - 2 bridges: `br1`, `br2`
- **Addressing**:
  - Router–router link uses automatic `/30` from `addressing.p2p` (see YAML; optional per-link `ipv4` overrides).
  - Access links (hosts and bridges) use automatic `/24` LAN subnets from `10.2.0.0/16`.
- **Node extras**: `h1` sets `sysctl`, `env`, and `exec` (startup script after deploy).
- **Routing**:
  - `routing.auto_static: true` enables BFS-based automatic static routing between routers.
- **Mgmt**:
  - `mgmt.enable: true` creates `netnslab-mgmt` bridge and per-node `eth0` interfaces.

Usage:

```bash
sudo ./netnslab deploy -f examples/demo-lab.yaml --debug
sudo ./netnslab enter demo-lab r1
sudo ./netnslab destroy demo-lab
```

### 2. `simple-2host-1router.yaml`

- **Lab name**: `simple-lab`
- **Topology**:
  - 2 hosts: `h1`, `h2`
  - 1 router: `r1`
- **Addressing**:
  - All links use automatic `/24` LAN subnets from `10.20.0.0/16`.
- **Routing**:
  - `routing.auto_static: false` – simple single-router lab, useful for basic IP connectivity tests.
- **Mgmt**:
  - `mgmt.enable: true` – management access to all nodes.

Usage:

```bash
sudo ./netnslab deploy -f examples/simple-2host-1router.yaml
sudo ./netnslab enter simple-lab r1
sudo ./netnslab destroy simple-lab
```

### 3. `manual-ip-ring-3routers.yaml`

- **Lab name**: `ring3-lab`
- **Topology**:
  - 3 routers: `r1`, `r2`, `r3` in a ring.
- **Addressing**:
  - All router-to-router links are manually configured `/30` subnets under `10.30.0.0/16`.
- **Routing**:
  - `routing.auto_static: true` – good for exercising the BFS static routing engine on a small ring.
- **Mgmt**:
  - `mgmt.enable: false` – access routers via host tools only (e.g. `ip netns exec`) or extend later.

Usage:

```bash
sudo ./netnslab deploy -f examples/manual-ip-ring-3routers.yaml
sudo ./netnslab enter ring3-lab r1
sudo ./netnslab destroy ring3-lab
```

### 4. `bridge-only-lan.yaml`

- **Lab name**: `bridge-lab`
- **Topology**:
  - 3 hosts: `h1`, `h2`, `h3`
  - 1 bridge: `br1`
- **Addressing**:
  - All host-to-bridge links use automatic `/24` LAN subnets from `10.60.0.0/16`.
- **Routing**:
  - `routing.auto_static: false` – pure L2 domain behind `br1`.
- **Mgmt**:
  - `mgmt.enable: true` – management bridge created for nodes.

Usage:

```bash
sudo ./netnslab deploy -f examples/bridge-only-lan.yaml
sudo ./netnslab enter bridge-lab h1
sudo ./netnslab destroy bridge-lab
```

