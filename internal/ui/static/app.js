const state = {
  labs: [],
  lab: "",
  topology: null,
  timer: null,
  selectedNode: "",
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

function colorByKind(kind) {
  if (kind === "router") return "#f39c12";
  if (kind === "bridge") return "#27ae60";
  return "#3498db"; // host default
}

function setStatus(text) {
  $("status").textContent = text;
}

function renderNodes() {
  const list = $("nodesList");
  list.innerHTML = "";
  const nodes = state.topology?.nodes || [];
  nodes.forEach((n) => {
    const btn = document.createElement("button");
    btn.className = "node-item";
    if (n.name === state.selectedNode) btn.classList.add("active");
    btn.onclick = () => {
      state.selectedNode = n.name;
      renderDetail();
      // re-render nodes to update active style
      renderNodes();
      renderGraph();
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
    $("detailBody").textContent = "无节点";
    return;
  }
  state.selectedNode = n.name;

  const ifaces = n.ifaces || [];
  const ifaceLines = ifaces.length
    ? ifaces.map((i) => `- ${i.ifname}: ${i.ipv4 || "-"}`).join("\n")
    : "- (无接口信息)";

  $("detailBody").innerHTML = `
    <p><span class="tag">${n.kind}</span></p>
    <p><strong>节点：</strong>${n.name}</p>
    <p><strong>mgmt(${n.has_mgmt ? "eth0" : "-"})：</strong>${n.mgmt_ip || "-"}</p>
    <pre>${ifaceLines}</pre>
  `;
}

function renderGraph() {
  const svg = $("topologySvg");
  svg.innerHTML = "";
  const nodes = state.topology?.nodes || [];
  const links = state.topology?.links || [];
  if (!nodes.length) return;

  const w = svg.clientWidth || 900;
  const h = svg.clientHeight || 640;
  const cx = w / 2;
  const cy = h / 2;
  const r = Math.min(w, h) * 0.35;

  // positions on a circle
  const pos = {};
  nodes.forEach((n, i) => {
    const a = (Math.PI * 2 * i) / nodes.length - Math.PI / 2;
    pos[n.name] = {
      x: cx + r * Math.cos(a),
      y: cy + r * Math.sin(a),
      kind: n.kind,
    };
  });

  // edges
  links.forEach((l) => {
    const a = pos[l.a.node];
    const b = pos[l.b.node];
    if (!a || !b) return;

    const line = document.createElementNS("http://www.w3.org/2000/svg", "line");
    line.setAttribute("x1", a.x);
    line.setAttribute("y1", a.y);
    line.setAttribute("x2", b.x);
    line.setAttribute("y2", b.y);
    line.setAttribute("stroke", "rgba(200,200,200,0.35)");
    line.setAttribute("stroke-width", "2");
    svg.appendChild(line);

    const midx = (a.x + b.x) / 2;
    const midy = (a.y + b.y) / 2;
    const label = document.createElementNS("http://www.w3.org/2000/svg", "text");
    label.setAttribute("x", midx);
    label.setAttribute("y", midy - 6);
    label.setAttribute("text-anchor", "middle");
    label.setAttribute("fill", "rgba(232,238,252,0.85)");
    label.setAttribute("font-size", "12");
    label.textContent = l.netem && l.netem !== "-" ? l.netem : `${l.a.ifname}<->${l.b.ifname}`;
    svg.appendChild(label);
  });

  // nodes
  nodes.forEach((n) => {
    const p = pos[n.name];
    if (!p) return;

    const g = document.createElementNS("http://www.w3.org/2000/svg", "g");

    const c = document.createElementNS("http://www.w3.org/2000/svg", "circle");
    c.setAttribute("cx", p.x);
    c.setAttribute("cy", p.y);
    c.setAttribute("r", n.name === state.selectedNode ? "26" : "22");
    c.setAttribute("fill", colorByKind(n.kind));
    c.setAttribute("opacity", n.name === state.selectedNode ? "1" : "0.9");
    c.setAttribute("stroke", "rgba(255,255,255,0.4)");
    c.setAttribute("stroke-width", n.name === state.selectedNode ? "3" : "1");

    const t = document.createElementNS("http://www.w3.org/2000/svg", "text");
    t.setAttribute("x", p.x);
    t.setAttribute("y", p.y + 4);
    t.setAttribute("text-anchor", "middle");
    t.setAttribute("fill", "#071020");
    t.setAttribute("font-weight", "700");
    t.setAttribute("font-size", "12");
    t.textContent = n.name;

    g.appendChild(c);
    g.appendChild(t);
    g.style.cursor = "pointer";
    g.onclick = () => {
      state.selectedNode = n.name;
      renderDetail();
      renderNodes();
      renderGraph();
    };

    svg.appendChild(g);
  });
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

async function loadTopology() {
  if (!state.lab) return;
  $("graphHint").textContent = "（刷新中...）";
  state.topology = await apiGet(`/api/labs/${encodeURIComponent(state.lab)}/topology`);
  state.selectedNode = state.selectedNode || (state.topology.nodes?.[0]?.name || "");

  renderNodes();
  renderDetail();
  renderGraph();

  const d = new Date();
  setStatus(`更新时间: ${d.toLocaleTimeString()}`);
  $("graphHint").textContent = "（MVP 圆形布局）";
}

function setupEvents() {
  $("labSelect").onchange = async (e) => {
    state.lab = e.target.value;
    state.selectedNode = "";
    await loadTopology();
  };
  $("refreshBtn").onclick = loadTopology;
  $("autoRefresh").onchange = (e) => {
    if (e.target.checked) {
      if (state.timer) return;
      state.timer = setInterval(loadTopology, 2000);
    } else {
      if (state.timer) clearInterval(state.timer);
      state.timer = null;
    }
  };
}

async function init() {
  setupEvents();
  await loadLabs();
  await loadTopology();
  // default auto refresh
  const auto = $("autoRefresh").checked;
  if (auto) state.timer = setInterval(loadTopology, 2000);
}

init().catch((err) => {
  setStatus("加载失败");
  console.error(err);
  $("detailBody").textContent = String(err?.message || err);
});

