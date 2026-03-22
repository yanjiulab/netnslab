#!/usr/bin/env bash
# End-to-end connectivity check: h1 pings r1 on demo-lab (same bridge segment).
# Requires: root, built ./netnslab, iproute2.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run as root (sudo): $0" >&2
  exit 1
fi

NETNSLAB="${ROOT}/netnslab"
if [[ ! -x "$NETNSLAB" ]]; then
  echo "Building netnslab..."
  go build -o "$NETNSLAB" ./cmd/netnslab
fi

LAB="demo-lab"
R1_NS="netnslab-${LAB}-r1"
YAML="${ROOT}/examples/demo-lab.yaml"

cleanup() {
  "$NETNSLAB" destroy -f "$YAML" 2>/dev/null || true
}
# trap cleanup EXIT

cleanup
echo "==> deploy $YAML"
"$NETNSLAB" deploy -f "$YAML"

# r1 attaches to br1 on eth2 in demo-lab.yaml
echo "==> discover r1 eth2 IPv4"
R1_IP="$(
  ip netns exec "$R1_NS" ip -4 -o addr show dev eth2 2>/dev/null | awk '{print $4}' | head -1 | cut -d/ -f1
)"
if [[ -z "$R1_IP" ]]; then
  echo "FAILED: could not read r1 eth2 address" >&2
  ip netns exec "$R1_NS" ip addr >&2 || true
  exit 1
fi
echo "    r1 eth2 = $R1_IP"

echo "==> ping from h1 to r1 ($R1_IP)"
if ! "$NETNSLAB" exec "$LAB" h1 -- ping -c 3 -W 2 "$R1_IP"; then
  echo "FAILED: h1 could not ping r1" >&2
  exit 1
fi

echo "OK: h1 -> r1 ping succeeded"
