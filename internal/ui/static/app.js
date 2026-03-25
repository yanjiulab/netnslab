const state = {
  labs: [],
  lab: "",
  topology: null,
  timer: null,
  selectedNode: "",
  live: true,
  cy: null,
  cyLayoutHash: "",
  layoutName: "cose",
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

function setStatus(text) {
  $("status").textContent = text;
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
  return `${node.name}\n(${node.kind})`;
}

function edgeLabel(link) {
  return `${link.a.ifname}<->${link.b.ifname}`;
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
          "font-size": 11,
          "font-weight": 700,
          "text-wrap": "wrap",
          "text-max-width": 90,
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
          label: "data(label)",
          "font-size": 10,
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
    highlightSelection();
    renderNodes();
    if (state.live) {
      await loadTopology(name);
      return;
    }
    renderDetail();
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
      label: edgeLabel(l),
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

  // Tooltips for edges (netem only).
  cy.edges().forEach((e) => {
    const netem = e.data("netem");
    const tt = netem && netem !== "-" ? `netem: ${netem}` : `${e.data("ifA")}<->${e.data("ifB")}`;
    e.data("tooltip", tt);
  });

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
  const nodes = state.topology?.nodes || [];
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
      <div class="node-sub">mgmt: ${n.mgmt_ip || "-"}</div>
    `;
    list.appendChild(btn);
  });
}

function renderDetail() {
  const nodes = state.topology?.nodes || [];
  const n = nodes.find((x) => x.name === state.selectedNode) || nodes[0];
  if (!n) {
    $("detailBody").textContent = "No node";
    return;
  }
  state.selectedNode = n.name;

  const mgmtLine = `mgmt eth0: ${n.mgmt_ip || "-"}`;
  const routes = Array.isArray(n.routes) ? n.routes : [];
  const routesBlock = routes.length ? routes.join("\n") : "-";
  const ifaces = n.ifaces || [];
  const ifaceLines = ifaces.length
    ? ifaces
        .map((i) => {
          const stateStr = state.live ? (i.up ? "UP" : "DOWN") : "-";
          const oper = state.live && i.operstate ? ` (${i.operstate})` : "";
          return `- ${i.ifname}: ${i.ipv4 || "-"} [${stateStr}]${oper}`;
        })
        .join("\n")
    : "-";

  let tcBlock = "";
  if (ifaces.some((i) => i.tc)) {
    const chunks = ifaces
      .filter((i) => i.tc && i.tc.trim().length)
      .map((i) => `\n--- tc qdisc: ${i.ifname} ---\n${i.tc}`);
    tcBlock = chunks.join("\n");
  }

  $("detailBody").innerHTML = `
    <p><span class="tag">${n.kind}</span></p>
    <p><strong>Node:</strong> ${n.name}</p>
    <p><strong>${mgmtLine}</strong></p>
    <h4 class="section-title">Routing table</h4>
    <pre>${escapeHtml(routesBlock)}</pre>
    <h4 class="section-title">Interfaces (up/down)</h4>
    <pre>${escapeHtml(ifaceLines)}</pre>
    ${
      tcBlock
        ? `<h4 class="section-title">TC qdisc (live)</h4><pre>${escapeHtml(tcBlock)}</pre>`
        : ""
    }
  `;
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

async function loadTopology(forceNodeName) {
  if (!state.lab) return;
  state.live = getLiveEnabled();
  $("graphHint").textContent = "Loading...";
  const liveFlag = state.live ? 1 : 0;
  const nodeParam = forceNodeName !== undefined ? forceNodeName : state.selectedNode || "";
  const hadNodeParam = nodeParam.trim() !== "";

  state.topology = await apiGet(
    `/api/labs/${encodeURIComponent(state.lab)}/topology?live=${liveFlag}&node=${encodeURIComponent(nodeParam)}`
  );
  state.selectedNode =
    forceNodeName !== undefined
      ? forceNodeName
      : state.selectedNode || (state.topology.nodes?.[0]?.name || "");

  if (state.live && !hadNodeParam && state.selectedNode) {
    return loadTopology(state.selectedNode);
  }

  syncGraph();
  renderNodes();
  renderDetail();

  const d = new Date();
  setStatus(`Updated: ${d.toLocaleTimeString()} (live=${state.live ? "on" : "off"})`);
  $("graphHint").textContent = "Cytoscape (drag to pin, double-click to unpin)";
}

function setupEvents() {
  $("labSelect").onchange = async (e) => {
    state.lab = e.target.value;
    state.selectedNode = "";
    await loadTopology();
  };
  $("refreshBtn").onclick = loadTopology;
  $("relayoutBtn").onclick = () => runLayout(true);
  $("layoutSelect").onchange = (e) => {
    state.layoutName = e.target.value || "cose";
    runLayout(true);
  };
  $("autoRefresh").onchange = (e) => {
    if (e.target.checked) {
      if (state.timer) return;
      state.timer = setInterval(loadTopology, 2000);
    } else {
      if (state.timer) clearInterval(state.timer);
      state.timer = null;
    }
  };
  $("liveToggle").onchange = async () => {
    await loadTopology();
  };
}

async function init() {
  setupEvents();
  ensureCy();
  const ls = $("layoutSelect");
  if (ls) state.layoutName = ls.value || "cose";
  await loadLabs();
  await loadTopology();
  if ($("autoRefresh").checked) {
    state.timer = setInterval(loadTopology, 2000);
  }
}

init().catch((err) => {
  setStatus("Load failed");
  console.error(err);
  $("detailBody").textContent = String(err?.message || err);
});

