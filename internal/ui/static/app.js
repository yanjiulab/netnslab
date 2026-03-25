const state = {
  labs: [],
  lab: "",
  topology: null,
  timer: null,
  selectedNode: "",
  selectedEdgeId: "",
  live: true,
  cy: null,
  cyLayoutHash: "",
  layoutName: "cose",
  centerMode: "topo",
  terminalTabs: {}, // sessionId -> {ws, term, fit, hostEl, tabEl}
  terminalTabOrder: [], // ordered list of sessionIds
  terminalActiveNode: "", // actually stores active sessionId in terminal mode
  terminalSessionSeq: 0,
};

async function apiGet(url) {
  const res = await fetch(url, { cache: "no-store" });
  if (!res.ok) {
    const txt = await res.text().catch(() => "");
    throw new Error(`GET ${url} failed: ${res.status} ${res.statusText} ${txt}`.trim());
  }
  return res.json();
}

function $(id) {
  return document.getElementById(id);
}

function updateMainHeightVar() {
  const topbar = document.querySelector(".topbar");
  const topbarH = topbar ? topbar.offsetHeight : 72;
  const mainH = Math.max(240, window.innerHeight - topbarH - 2);
  document.documentElement.style.setProperty("--main-h", `${mainH}px`);
}

function setStatus(text) {
  $("status").textContent = text;
}

function edgeFloatEl() {
  return $("edgeFloat");
}

function hideEdgeFloat() {
  const el = edgeFloatEl();
  if (!el) return;
  el.textContent = "";
  el.classList.add("hidden");
}

function showEdgeFloat(text, clientX, clientY) {
  const el = edgeFloatEl();
  if (!el || !text) return;
  el.textContent = text;
  el.classList.remove("hidden");
  const x = Math.max(8, Math.min(clientX + 12, window.innerWidth - 460));
  const y = Math.max(8, Math.min(clientY + 12, window.innerHeight - 160));
  el.style.left = `${x}px`;
  el.style.top = `${y}px`;
}

function colorByKind(kind) {
  if (kind === "router") return "#f39c12";
  if (kind === "bridge") return "#27ae60";
  return "#3498db";
}

function escapeHtml(s) {
  return String(s)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function getLiveEnabled() {
  const el = $("liveToggle");
  return el ? !!el.checked : true;
}

function nodeLabel(node) {
  // In topology graph, only show the node name (e.g. R1).
  return `${node.name}`;
}

function edgeLabel(link) {
  return `${link.a.ifname}<->${link.b.ifname}`;
}

function edgeFullLabel(link) {
  const left = `${link.a.node}:${link.a.ifname}`;
  const right = `${link.b.node}:${link.b.ifname}`;
  const base = `${left}<->${right}`;
  if (link.netem && link.netem !== "-") {
    return `${base}\nnetem: ${link.netem}`;
  }
  return base;
}

function makeNodeId(name) {
  return `n:${name}`;
}

function makeEdgeId(link) {
  return `e:${link.a.node}:${link.a.ifname}-${link.b.node}:${link.b.ifname}`;
}

function layoutHash(topo) {
  if (!topo) return "";
  const ns = (topo.nodes || []).map((n) => `${n.name}:${n.kind}`).sort().join("|");
  const ls = (topo.links || [])
    .map((l) => `${l.a.node}:${l.a.ifname}<->${l.b.node}:${l.b.ifname}`)
    .sort()
    .join("|");
  return `n=${ns};l=${ls}`;
}

function ensureCy() {
  if (state.cy) return state.cy;
  if (typeof cytoscape !== "function") {
    throw new Error("cytoscape is not loaded");
  }

  state.cy = cytoscape({
    container: $("topologyGraph"),
    elements: [],
    style: [
      {
        selector: "node",
        style: {
          "background-color": "data(color)",
          label: "data(label)",
          color: "#071020",
          "font-size": 13,
          "font-weight": 700,
          "text-wrap": "wrap",
          "text-max-width": 110,
          "text-valign": "center",
          "text-halign": "center",
          width: 40,
          height: 40,
          "border-width": 1.5,
          "border-color": "rgba(255,255,255,0.45)",
        },
      },
      {
        selector: "node.selected",
        style: {
          width: 48,
          height: 48,
          "border-width": 3,
          "border-color": "rgba(255,255,255,0.85)",
        },
      },
      {
        selector: "node.pinned",
        style: {
          "border-style": "double",
          "border-width": 4,
          "border-color": "rgba(255,255,255,0.95)",
        },
      },
      {
        selector: "edge",
        style: {
          width: 2,
          "line-color": "rgba(200,200,200,0.35)",
          label: "",
          "font-size": 12,
          color: "rgba(232,238,252,0.85)",
          "text-background-opacity": 0.2,
          "text-background-color": "#121a2b",
          "text-background-padding": 2,
          "curve-style": "bezier",
        },
      },
      {
        selector: "edge.selected",
        style: {
          width: 3,
          "line-color": "rgba(255,255,255,0.6)",
        },
      },
    ],
    userPanningEnabled: true,
    userZoomingEnabled: true,
    boxSelectionEnabled: false,
    autoungrabify: false,
    autounselectify: false,
    wheelSensitivity: 0.22,
  });

  // Node click -> select node and refresh selected-node live info.
  state.cy.on("tap", "node", async (evt) => {
    const ele = evt.target;
    const name = ele.data("name");
    if (!name) return;
    state.selectedNode = name;
    state.selectedEdgeId = "";
    hideEdgeFloat();
    highlightSelection();
    renderNodes();
    if (state.live) {
      await loadTopology(name);
      return;
    }
    renderDetail();
  });

  // Edge click -> show full link info (do not render link label on canvas).
  state.cy.on("tap", "edge", (evt) => {
    if (state.centerMode !== "topo") return;
    const edge = evt.target;
    const id = edge.id();
    const full =
      edge.data("fullLabel") ||
      `${edge.data("ifA")}<->${edge.data("ifB")}` +
        (edge.data("netem") && edge.data("netem") !== "-"
          ? `\nnetem: ${edge.data("netem")}`
          : "");

    if (state.selectedEdgeId === id) {
      state.selectedEdgeId = "";
      hideEdgeFloat();
      try {
        edge.unselect();
      } catch (_) {}
      return;
    }

    state.selectedEdgeId = id;
    edge.select();
    let clientX = window.innerWidth / 2;
    let clientY = window.innerHeight / 2;
    const oe = evt.originalEvent;
    if (oe && typeof oe.clientX === "number" && typeof oe.clientY === "number") {
      clientX = oe.clientX;
      clientY = oe.clientY;
    }
    showEdgeFloat(full, clientX, clientY);
  });

  // Click background -> clear edge info.
  state.cy.on("tap", (evt) => {
    if (evt.target === state.cy) {
      state.selectedEdgeId = "";
      hideEdgeFloat();
    }
  });

  // Drag to pin.
  state.cy.on("dragfree", "node", (evt) => {
    const ele = evt.target;
    ele.data("pinned", true);
    ele.addClass("pinned");
  });

  // Double click to unpin.
  let lastTap = 0;
  state.cy.on("tap", "node", (evt) => {
    const now = Date.now();
    const delta = now - lastTap;
    lastTap = now;
    if (delta < 300) {
      const ele = evt.target;
      ele.data("pinned", false);
      ele.removeClass("pinned");
      ele.unlock();
      runLayout(false);
    }
  });

  return state.cy;
}

function runLayout(force) {
  const cy = ensureCy();
  // For regular refresh, preserve current positions and run a gentle layout only if forced/topology changed.
  if (!force) return;
  const name = state.layoutName || "cose";
  let opts = {
    name,
    animate: true,
    animationDuration: 350,
    fit: true,
    padding: 30,
  };
  if (name === "cose") {
    opts = {
      ...opts,
      randomize: false,
      nodeRepulsion: 450000,
      idealEdgeLength: 120,
      edgeElasticity: 100,
      gravity: 0.2,
      numIter: 500,
    };
  } else if (name === "circle") {
    opts = { ...opts, spacingFactor: 1.1 };
  } else if (name === "grid") {
    opts = { ...opts, avoidOverlap: true };
  } else if (name === "concentric") {
    opts = {
      ...opts,
      concentric: (n) => n.connectedEdges().length,
      levelWidth: () => 2,
    };
  } else if (name === "breadthfirst") {
    opts = { ...opts, directed: false, spacingFactor: 1.1 };
  }
  const layout = cy.layout(opts);
  layout.run();
}

function syncGraph() {
  const cy = ensureCy();
  const topo = state.topology || { nodes: [], links: [] };
  const nodes = topo.nodes || [];
  const links = topo.links || [];

  const existingIds = new Set(cy.elements().map((e) => e.id()));
  const wantIds = new Set();

  // Upsert nodes.
  nodes.forEach((n) => {
    const id = makeNodeId(n.name);
    wantIds.add(id);
    const data = {
      id,
      name: n.name,
      kind: n.kind,
      label: nodeLabel(n),
      color: colorByKind(n.kind),
    };
    const ele = cy.getElementById(id);
    if (ele.nonempty()) {
      ele.data(data);
    } else {
      cy.add({ group: "nodes", data });
    }
  });

  // Upsert edges.
  links.forEach((l) => {
    const id = makeEdgeId(l);
    wantIds.add(id);
    const data = {
      id,
      source: makeNodeId(l.a.node),
      target: makeNodeId(l.b.node),
      label: edgeLabel(l), // used for fallback/other tooling; edge label display is controlled by Cytoscape styles
      fullLabel: edgeFullLabel(l),
      netem: l.netem || "-",
      ifA: l.a.ifname,
      ifB: l.b.ifname,
    };
    const ele = cy.getElementById(id);
    if (ele.nonempty()) {
      ele.data(data);
    } else {
      cy.add({ group: "edges", data });
    }
  });

  // Remove stale.
  existingIds.forEach((id) => {
    if (!wantIds.has(id)) {
      const ele = cy.getElementById(id);
      if (ele.nonempty()) ele.remove();
    }
  });

  // Keep pinned nodes locked.
  cy.nodes().forEach((n) => {
    if (n.data("pinned")) n.lock();
    else n.unlock();
  });

  // Note: we intentionally do not render edge labels on canvas.
  // Full link info is shown on edge click via the `edgeInfo` overlay.

  // Layout only if topology changed.
  const h = layoutHash(topo);
  const changed = h !== state.cyLayoutHash;
  if (changed) {
    state.cyLayoutHash = h;
    runLayout(true);
  }

  highlightSelection();
}

function highlightSelection() {
  const cy = ensureCy();
  cy.elements().removeClass("selected");
  if (!state.selectedNode) return;
  const nodeId = makeNodeId(state.selectedNode);
  const n = cy.getElementById(nodeId);
  if (n.nonempty()) {
    n.addClass("selected");
    n.connectedEdges().addClass("selected");
  }
}

function renderNodes() {
  const list = $("nodesList");
  list.innerHTML = "";
  const nodes =
    state.topology && state.topology.nodes ? state.topology.nodes : [];
  nodes.forEach((n) => {
    const btn = document.createElement("button");
    btn.className = "node-item";
    if (n.name === state.selectedNode) btn.classList.add("active");
    btn.onclick = async () => {
      state.selectedNode = n.name;
      highlightSelection();
      renderNodes();
      if (state.live) {
        await loadTopology(n.name);
        return;
      }
      renderDetail();
    };
    btn.innerHTML = `
      <div><strong>${n.name}</strong> <span class="tag">${n.kind}</span></div>
    `;
    btn.ondblclick = () => {
      if (state.centerMode !== "terminal") return;
      openTerminalForNode(n.name);
    };
    list.appendChild(btn);
  });
}

function renderDetail() {
  const nodes =
    state.topology && state.topology.nodes ? state.topology.nodes : [];
  const n = nodes.find((x) => x.name === state.selectedNode) || nodes[0];
  if (!n) {
    $("detailBody").textContent = "No node";
    return;
  }
  state.selectedNode = n.name;
  const routes = Array.isArray(n.routes) ? n.routes : [];
  const routesBlock = routes.length
    ? routes
        .map((r) => {
          const isDefault = r.startsWith("default ");
          const metricMatch = r.match(/\bmetric\s+(\d+)/);
          const parts = [];
          if (metricMatch && metricMatch[1]) parts.push(`metric=${metricMatch[1]}`);
          const suffix = parts.length ? `  [${parts.join(", ")}]` : "";
          return `${isDefault ? "* " : "  "}${r}${suffix}`;
        })
        .join("\n")
    : "-";
  const ifaces = n.ifaces || [];
  const ifaceLines = ifaces.length
    ? ifaces
        .map((i) => {
          const stateStr = state.live ? (i.up ? "UP" : "DOWN") : "-";
          const oper = state.live && i.operstate ? ` (${i.operstate})` : "";
          const mac = i.mac ? ` mac=${i.mac}` : "";
          const mtu = i.mtu ? ` mtu=${i.mtu}` : "";
          const hasStats = i.rx_packets !== undefined || i.tx_packets !== undefined;
          const rxPackets = i.rx_packets !== undefined ? i.rx_packets : 0;
          const rxBytes = i.rx_bytes !== undefined ? i.rx_bytes : 0;
          const rxDropped = i.rx_dropped !== undefined ? i.rx_dropped : 0;
          const rxErrors = i.rx_errors !== undefined ? i.rx_errors : 0;
          const txPackets = i.tx_packets !== undefined ? i.tx_packets : 0;
          const txBytes = i.tx_bytes !== undefined ? i.tx_bytes : 0;
          const txDropped = i.tx_dropped !== undefined ? i.tx_dropped : 0;
          const txErrors = i.tx_errors !== undefined ? i.tx_errors : 0;
          const rx = ` rx[pkt=${rxPackets} bytes=${rxBytes} drop=${rxDropped} err=${rxErrors}]`;
          const tx = ` tx[pkt=${txPackets} bytes=${txBytes} drop=${txDropped} err=${txErrors}]`;
          const statsFlag = ` stats=${hasStats ? "present" : "missing"}`;
          return `- ${i.ifname}: ${i.ipv4 || "-"} [${stateStr}]${oper}${mac}${mtu}${rx}${tx}${statsFlag}`;
        })
        .join("\n")
    : "-";
  const neigh = Array.isArray(n.neigh) ? n.neigh : [];
  const neighBlock = neigh.length ? neigh.join("\n") : "-";

  $("detailBody").innerHTML = `
    <p><strong>Node:</strong> ${n.name}</p>
    <h4 class="section-title">Routing table</h4>
    <pre>${escapeHtml(routesBlock)}</pre>
    <h4 class="section-title">Interfaces (up/down)</h4>
    <pre>${escapeHtml(ifaceLines)}</pre>
    <h4 class="section-title">Neighbors (ARP/NDP)</h4>
    <pre>${escapeHtml(neighBlock)}</pre>
  `;
}

function setCenterMode(mode) {
  state.centerMode = mode === "terminal" ? "terminal" : "topo";
  updateMainHeightVar();

  const topoView = $("topoView");
  const terminalView = $("terminalView");
  if (topoView) topoView.classList.toggle("hidden", state.centerMode !== "topo");
  if (terminalView)
    terminalView.classList.toggle("hidden", state.centerMode !== "terminal");

  if (state.centerMode !== "topo") {
    state.selectedEdgeId = "";
    hideEdgeFloat();
  }

  // Resize graph when becoming visible.
  if (state.centerMode === "topo") {
    // Run on next frame so the container has final size.
    requestAnimationFrame(() => {
      try {
        if (state.cy) {
          state.cy.resize();
          runLayout(false);
        }
      } catch (_) {}
    });
  } else {
    // Ensure active terminal is visible & properly sized.
    if (state.terminalActiveNode || state.terminalTabOrder.length) {
      const pick =
        state.terminalActiveNode ||
        state.terminalTabOrder[0] ||
        "";
      if (pick) {
        selectTerminalTab(pick);
        requestAnimationFrame(() => {
          terminalSendResizeForSession(pick);
        });
      }
    }
  }
}

function terminalTabsEl() {
  return $("terminalTabs");
}

function terminalTabsBodyEl() {
  return $("terminalTabsBody");
}

function terminalEmptyEl() {
  return $("terminalEmpty");
}

function terminalHostId(sessionId) {
  return `terminalHost_${encodeURIComponent(sessionId)}`;
}

function terminalTabId(sessionId) {
  return `terminalTab_${encodeURIComponent(sessionId)}`;
}

function terminalSendResizeForSession(sessionId) {
  const tab = state.terminalTabs[sessionId];
  if (!tab || !tab.ws || !tab.term) return;
  if (tab.ws.readyState !== WebSocket.OPEN) return;
  tab.ws.send(
    JSON.stringify({
      type: "resize",
      cols: tab.term.cols,
      rows: tab.term.rows,
    })
  );
}

function ensureTerminalEmptyState() {
  const el = terminalEmptyEl();
  if (!el) return;
  const cnt = state.terminalTabOrder.length;
  el.classList.toggle("hidden", cnt > 0);
}

function selectTerminalTab(sessionId) {
  if (!state.terminalTabs[sessionId]) return;
  state.terminalActiveNode = sessionId;

  // Update tab active styles and show only active host.
  Object.keys(state.terminalTabs).forEach((k) => {
    const tab = state.terminalTabs[k];
    if (!tab) return;
    const active = k === sessionId;
    if (tab.tabEl) tab.tabEl.classList.toggle("active", active);
    if (tab.hostEl) tab.hostEl.classList.toggle("hidden", !active);
  });

  // Fit after becoming visible.
  try {
    const tab = state.terminalTabs[sessionId];
    if (tab && tab.fit) tab.fit.fit();
    terminalSendResizeForSession(sessionId);
    if (tab && tab.term) tab.term.focus();
  } catch (_) {}
}

function closeTerminalForSession(sessionId) {
  const tab = state.terminalTabs[sessionId];
  if (!tab) return;

  // Mark as closing so handlers won't try to touch DOM after removal.
  tab.closing = true;

  try {
    if (tab.ws) tab.ws.close();
  } catch (_) {}
  try {
    if (tab.ro) tab.ro.disconnect();
  } catch (_) {}
  try {
    if (tab.term) tab.term.dispose();
  } catch (_) {}

  if (tab.tabEl && tab.tabEl.parentNode) tab.tabEl.parentNode.removeChild(tab.tabEl);
  if (tab.hostEl && tab.hostEl.parentNode) tab.hostEl.parentNode.removeChild(tab.hostEl);

  delete state.terminalTabs[sessionId];
  state.terminalTabOrder = state.terminalTabOrder.filter((x) => x !== sessionId);

  if (state.terminalActiveNode === sessionId) {
    state.terminalActiveNode = state.terminalTabOrder[0] || "";
    if (state.centerMode === "terminal" && state.terminalActiveNode) {
      selectTerminalTab(state.terminalActiveNode);
    }
  }

  ensureTerminalEmptyState();
}

function closeAllTerminalSessions() {
  const ids = state.terminalTabOrder.slice();
  ids.forEach((sid) => closeTerminalForSession(sid));
  state.terminalActiveNode = "";
}

async function openTerminalForNode(nodeName) {
  if (!state.lab) {
    setStatus("No lab loaded");
    return;
  }
  if (!nodeName) return;

  // Always create a new terminal session (same node can have multiple tabs).
  const sessionId = `${state.lab}:${nodeName}#${++state.terminalSessionSeq}`;

  const hostArea = terminalTabsBodyEl();
  const tabsArea = terminalTabsEl();
  if (!hostArea || !tabsArea) {
    setStatus("Terminal UI not ready");
    return;
  }

  // Create tab + host container.
  const tabEl = document.createElement("div");
  tabEl.className = "terminal-tab";
  tabEl.id = terminalTabId(sessionId);
  tabEl.dataset.node = nodeName;

  const titleEl = document.createElement("div");
  titleEl.className = "terminal-tab-title";
  titleEl.textContent = `${nodeName}@${state.lab}`;

  const closeBtn = document.createElement("button");
  closeBtn.className = "terminal-tab-close";
  closeBtn.type = "button";
  closeBtn.textContent = "×";
  closeBtn.onclick = (ev) => {
    ev.stopPropagation();
    closeTerminalForSession(sessionId);
  };

  tabEl.appendChild(titleEl);
  tabEl.appendChild(closeBtn);
  tabEl.onclick = () => {
    setCenterMode("terminal");
    selectTerminalTab(sessionId);
  };

  const hostEl = document.createElement("div");
  hostEl.className = "terminal-tab-host hidden";
  hostEl.id = terminalHostId(sessionId);

  tabsArea.appendChild(tabEl);
  hostArea.appendChild(hostEl);

  const term = new Terminal({
    cursorBlink: true,
    convertEol: true,
    scrollback: 2000,
    fontSize: 13,
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(hostEl);

  // Create websocket.
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const wsUrl = `${proto}://${location.host}/ws/labs/${encodeURIComponent(
    state.lab
  )}/nodes/${encodeURIComponent(nodeName)}/terminal`;
  const ws = new WebSocket(wsUrl);

  const encoder = new TextEncoder();
  const decoder = new TextDecoder();

  const tab = {
    ws: ws,
    term: term,
    fit: fit,
    hostEl: hostEl,
    tabEl: tabEl,
    ro: null,
    closing: false,
    nodeName: nodeName,
  };
  state.terminalTabs[sessionId] = tab;
  state.terminalTabOrder.push(sessionId);

  ensureTerminalEmptyState();
  selectTerminalTab(sessionId);

  ws.onopen = () => {
    try {
      fit.fit();
      terminalSendResizeForSession(sessionId);
    } catch (_) {}
    term.focus();
  };

  ws.onmessage = async (ev) => {
    if (!state.terminalTabs[sessionId] || state.terminalTabs[sessionId].closing)
      return;
    const data = ev.data;
    if (typeof data === "string") {
      term.write(data);
      return;
    }
    if (data instanceof ArrayBuffer) {
      term.write(decoder.decode(new Uint8Array(data)));
      return;
    }
    if (data instanceof Blob) {
      const buf = await data.arrayBuffer();
      term.write(decoder.decode(new Uint8Array(buf)));
      return;
    }
  };

  ws.onerror = () => {
    setStatus("WebSocket error");
  };

  ws.onclose = () => {
    // If closed via tab button, cleanup already started.
    if (!state.terminalTabs[sessionId]) return;
    if (tab.closing) return;
    setStatus("Terminal closed");
    closeTerminalForSession(sessionId);
  };

  term.onData((d) => {
    const t = state.terminalTabs[sessionId];
    if (!t || !t.ws || t.ws.readyState !== WebSocket.OPEN) return;
    const bytes = encoder.encode(d);
    t.ws.send(bytes);
  });

  // Keep PTY size in sync with this tab's container.
  tab.ro = new ResizeObserver(() => {
    const t = state.terminalTabs[sessionId];
    if (!t) return;
    if (t.hostEl.classList.contains("hidden")) return;
    try {
      fit.fit();
      terminalSendResizeForSession(sessionId);
    } catch (_) {}
  });
  tab.ro.observe(hostEl);
}

async function loadLabs() {
  const data = await apiGet("/api/labs");
  state.labs = data.labs || [];
  const sel = $("labSelect");
  sel.innerHTML = "";
  state.labs.forEach((l) => {
    const o = document.createElement("option");
    o.value = l.name;
    o.textContent = l.name;
    sel.appendChild(o);
  });

  if (!state.lab && state.labs.length) {
    state.lab = state.labs[0].name;
    sel.value = state.lab;
  } else if (state.lab) {
    sel.value = state.lab;
  }
}

async function loadTopology(forceNodeName, options) {
  const opts = options || {};
  const skipGraphRefresh = !!opts.skipGraphRefresh;
  const silent = !!opts.silent;
  if (!state.lab) return;
  state.live = getLiveEnabled();
  updateMainHeightVar();
  if (!silent) $("graphHint").textContent = "Loading...";
  const liveFlag = state.live ? 1 : 0;
  const nodeParam = forceNodeName !== undefined ? forceNodeName : state.selectedNode || "";
  const hadNodeParam = nodeParam.trim() !== "";

  state.topology = await apiGet(
    `/api/labs/${encodeURIComponent(state.lab)}/topology?live=${liveFlag}&node=${encodeURIComponent(nodeParam)}`
  );
  state.selectedNode =
    forceNodeName !== undefined
      ? forceNodeName
      : state.selectedNode ||
        ((state.topology &&
          state.topology.nodes &&
          state.topology.nodes[0] &&
          state.topology.nodes[0].name) ||
          "");

  if (state.live && !hadNodeParam && state.selectedNode) {
    return loadTopology(state.selectedNode, opts);
  }

  if (!skipGraphRefresh) {
    syncGraph();
  }
  renderNodes();
  renderDetail();

  const d = new Date();
  if (!silent) {
    setStatus(
      `Updated: ${d.toLocaleTimeString()} (live=${state.live ? "on" : "off"})`
    );
    $("graphHint").textContent =
      "Cytoscape (drag to pin, double-click to unpin)";
  }
  if (!skipGraphRefresh && state.centerMode === "topo") {
    requestAnimationFrame(() => {
      try {
        if (state.cy) state.cy.resize();
      } catch (_) {}
    });
  }
}

function setupEvents() {
  $("labSelect").onchange = async (e) => {
    closeAllTerminalSessions();
    state.lab = e.target.value;
    state.selectedNode = "";
    await loadTopology();
  };
  $("refreshBtn").onclick = () =>
    loadTopology(undefined, { skipGraphRefresh: true, silent: true });
  $("relayoutBtn").onclick = () => runLayout(true);
  $("layoutSelect").onchange = (e) => {
    state.layoutName = e.target.value || "cose";
    runLayout(true);
  };
  $("autoRefresh").onchange = (e) => {
    if (e.target.checked) {
      if (state.timer) return;
      state.timer = setInterval(() => loadTopology(undefined, { silent: true }), 2000);
    } else {
      if (state.timer) clearInterval(state.timer);
      state.timer = null;
    }
  };
  $("liveToggle").onchange = async () => {
    await loadTopology();
  };

  const viewSel = $("viewModeSelect");
  if (viewSel) {
    viewSel.onchange = () => setCenterMode(viewSel.value);
  }
}

async function init() {
  updateMainHeightVar();
  window.addEventListener("resize", () => {
    updateMainHeightVar();
    if (state.centerMode === "topo") {
      try {
        if (state.cy) state.cy.resize();
      } catch (_) {}
    } else if (state.terminalActiveNode) {
      terminalSendResizeForSession(state.terminalActiveNode);
    }
  });

  setupEvents();
  ensureCy();
  const ls = $("layoutSelect");
  if (ls) state.layoutName = ls.value || "cose";
  await loadLabs();
  await loadTopology();
  setCenterMode(state.centerMode);
  if ($("autoRefresh").checked) {
    state.timer = setInterval(loadTopology, 2000);
  }
}

init().catch((err) => {
  setStatus("Load failed");
  console.error(err);
  $("detailBody").textContent = String(err && err.message ? err.message : err);
});

