# netnslab

**netnslab** is a lightweight network lab tool for **Linux** built on **network namespaces** (`netns`). It does not use containers or images. You describe topology in a **YAML** file (containerlab-style), then deploy veth pairs, bridges, addressing, optional static routing, and a management network.

## Features

- **Node kinds**: `host`, `router`, `bridge` (Linux bridge `br0` inside the bridge netns).
- **Addressing**: `addressing.p2p` and `addressing.lan` as pools; automatic `/30` (P2P) and `/24` (LAN) allocation without overlap inside each pool; bridge ports stay L2-only (no IP).
- **Routing**: Optional BFS-based **auto static routes** on routers (`routing.auto_static`).
- **Management network**: Optional `mgmt` bridge on the host and per-node `eth0`.
- **Hosts**: Default route via the **local segment router** (same bridge or direct link); **routers** get `net.ipv4.ip_forward=1` unless overridden in YAML.
- **Link impairment**: Optional per-link **`netem`** (`delay_ms`, `jitter_ms`, `loss_percent`) via `tc` on **both** endpoints (needs `sch_netem` and `tc`).
- **Per-node options**: `sysctl` (applied in netns), **`env`** (written to `node-env.json` and merged into `exec` / `enter` / `capture`), **`exec`** (shell snippet run via `bash -c` after routes; inherits `env`).
- **CLI**: `deploy`, `destroy`, `enter`, `exec`, `list`, `show`, `graph`, `capture`, global `--debug`.
- **Runtime state**: After deploy, `lab-state.json` under the lab run directory drives `show` / `graph` (live IPs from `ip` in netns).

## Requirements

- **Linux** with network namespaces.
- **`ip`** from **iproute2** (for `ip netns`, `ip link`, `ip addr`, `ip route`).
- **`tc`** if you use link **`netem`** (typically `iproute2`; kernel module `sch_netem`).
- **Root** (or `sudo`) to create netns, veth, bridges, and routes.
- **Go 1.21+** to build from source.

## Build

```bash
./build.sh
```

## Quick start

```bash
sudo ./netnslab deploy -f examples/demo-lab.yaml --debug
sudo ./netnslab show demo-lab
sudo ./netnslab enter demo-lab h1
sudo ./netnslab graph demo-lab > /tmp/lab.dot
sudo ./netnslab destroy demo-lab
```

- **`deploy`** requires **`-f`** pointing at the topology YAML.
- **`destroy`**, **`show`**, **`graph`**, **`enter`**, **`exec`**, **`capture`** use the **lab name** (the `name:` field in YAML).

See [`examples/README.md`](examples/README.md) for more sample topologies and scripts.

## YAML overview

Top-level fields include `name`, `routing`, `addressing`, `mgmt`, and `topology` (`nodes`, `links`). Example:

```yaml
name: my-lab
routing:
  auto_static: true
addressing:
  p2p: 10.1.0.0/16
  lan: 10.2.0.0/16
mgmt:
  enable: true
  ipv4: 192.168.100.0/24
topology:
  nodes:
    h1: { kind: host }
    r1: { kind: router }
  links:
    - endpoints: ["h1:eth1", "r1:eth1"]
```

Optional **per-link netem**:

```yaml
links:
  - endpoints: ["h1:eth1", "r1:eth1"]
    netem:
      delay_ms: 20
      jitter_ms: 5
      loss_percent: 0.1
```

Optional **per-node** `sysctl`, `env`, and `exec` (see `examples/demo-lab.yaml`):

```yaml
topology:
  nodes:
    h1:
      kind: host
      sysctl:
        net.core.somaxconn: "65535"
      env:
        DEBUG: "1"
      exec: |
        ping -c 3 1.1.1.1 > /var/log/h1-ping.log 2>&1 &
```

Validation rejects unknown `kind` values and **overlapping IPv4** among `addressing.p2p`, `addressing.lan`, `addressing.loopback`, and `mgmt.ipv4` (when mgmt is enabled).

## Runtime paths

Default layout (see `internal/netns/paths.go`):

- State and per-node dirs: under **`/var/run/netnslab/<lab>/`** (includes `lab-state.json` and per-node `node-env.json` when `env` is set).
- Logs: **`/var/log/netnslab/<lab>/`**.

## CLI reference (short)

| Command | Description |
| ------- | ----------- |
| `deploy -f FILE` | Build lab from YAML |
| `validate -f FILE` | Validate YAML topology without deploying |
| `destroy LAB` | Tear down deployed lab by lab name |
| `list` | List deployed labs |
| `show LAB` | Live summary (IPs + link **netem** summary) |
| `graph LAB` | Graphviz DOT with live interface IPs |
| `enter LAB NODE` | Interactive shell in node netns |
| `exec LAB NODE -- CMD...` | Run command in one node netns |
| `exec LAB --all -- CMD...` | Run command on all nodes in lab |
| `exec LAB --kind router -- CMD...` | Run command on nodes filtered by kind |
| `capture LAB NODE IFACE` | `tcpdump` in netns (pcap under run dir) |

## Project layout

```text
cmd/netnslab/          # CLI entry
internal/cli/          # Cobra commands
internal/config/       # YAML types and validation
internal/topology/     # Build, addressing, bridge ports, host gateway
internal/netns/        # Namespaces, veth, bridge, routes, netem, ip queries
internal/routing/      # Auto static routes
internal/mgmt/         # Host management bridge
internal/labstate/     # Persisted lab-state.json
internal/logx/         # Zap logging
examples/              # Example YAML files
```

## License

MIT
