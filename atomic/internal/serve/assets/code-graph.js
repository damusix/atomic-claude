// code-graph.js — Code Graph view: the profile that wires the shared cosmos.gl
// engine (graph-core.js, loaded first — see layout.html) to a repo's
// /code/graph/data endpoint (the symbol graph from that repo's atomic.db).
// Served as a static asset (go:embed assets), loaded via
// <script src="/static/code-graph.js"> in layout.html, right after
// system-graph.js. See docs/spec/code-graph.md checkpoint 5 for the contract
// this file fulfills, and checkpoint 4 (graph-core.js's own header) for the
// core/profile split.
//
// Owns: the /code/graph/data fetch (with an optional ?member= prefix) +
// fingerprint-as-cache-key (namespaced "code:<member>:<fingerprint>" — a
// bare fingerprint would collide with the docs profile's own cache entries,
// which key on the SAME IndexedDB store), the flat-JSON → cosmos adapter, the
// kind→group palette (SC5: the 38 codeintel NodeKind values collapsed to ~8
// visual groups, reusing the docs profile's existing 8 theme-aware type
// colors — see GROUP_ALIAS below), edge-kind styling (contains subordinate,
// calls primary, imports secondary, everything else tertiary), and node
// meta/label resolution routed through AtomicGraphUI's engine-neutral
// preview-card hook (same hook the docs profile uses).
//
// Explorer click integration (SC6) and the Docs|Code switcher + member
// picker (SC7) are checkpoint 6's job — onClick below is a documented no-op
// until then. Everything else (mount/teardown/retheme lifecycle, WebGL2
// gate, motion policy, layout cache, label overlay, legend, drag handling,
// debugState()/simRunning()) lives in graph-core.js and is forwarded
// verbatim below, mirroring window.SystemGraph's own public shape.
window.CodeGraph = (function() {

  // ── Kind → group taxonomy (SC5) ────────────────────────────────────────────

  // KIND_GROUPS maps every codeintel NodeKind string (atomic/internal/codeintel/types/types.go's
  // AllNodeKinds, 38 values) to one of 8 visual groups. A kind absent from
  // this table — a future addition to types.go, or an unexpected server
  // value — falls through to the 'other' default bucket via kindToGroup, so
  // every node always resolves to a defined, colored group.
  var KIND_GROUPS = {
    // module-file — structural containers, not symbols themselves.
    file: 'module-file', module: 'module-file', namespace: 'module-file', component: 'module-file',
    // type — OOP/structural type definitions.
    'class': 'type', struct: 'type', interface: 'type', trait: 'type', protocol: 'type',
    'enum': 'type', enum_member: 'type', type_alias: 'type',
    // callable — executable code a caller invokes by name.
    'function': 'callable', method: 'callable', macro: 'callable',
    // value — data-carrying declarations, not executable.
    property: 'value', field: 'value', variable: 'value', constant: 'value', parameter: 'value',
    // sql-data — SQL/warehouse data objects.
    table: 'sql-data', view: 'sql-data', column: 'sql-data', index: 'sql-data', sequence: 'sql-data',
    constraint: 'sql-data', policy: 'sql-data', stage: 'sql-data', stream: 'sql-data',
    file_format: 'sql-data', model: 'sql-data',
    // sql-routine — executable SQL/orchestration objects.
    procedure: 'sql-routine', trigger: 'sql-routine', task: 'sql-routine', script: 'sql-routine',
    // import-export — cross-module/cross-file linkage declarations.
    'import': 'import-export', 'export': 'import-export', route: 'import-export'
  };

  function kindToGroup(kind) {
    return KIND_GROUPS[kind] || 'other';
  }

  // GROUP_ALIAS reuses the docs profile's existing 8 theme-aware type colors
  // (atomicCyTypeColors() in layout.html, sourced from app.css's --c-* custom
  // properties) rather than introducing new CSS — same reuse-over-rewrite
  // reasoning the OKF taxonomy itself was built on. Zero new CSS variables
  // means retheme() (light/dark) works for the code view for free.
  var GROUP_ALIAS = {
    'module-file': 'repo',
    'type': 'concern',
    callable: 'page',
    value: 'domain',
    'sql-data': 'knowledge',
    'sql-routine': 'bucket',
    'import-export': 'index',
    other: 'external'
  };

  // colors() re-reads atomicCyTypeColors() on every call (never cached) so a
  // theme flip picks up the new CSS vars — same contract graph-core.js's
  // applyStyling expects from every profile.colors().
  function colors() {
    var base = atomicCyTypeColors();
    var out = {};
    Object.keys(GROUP_ALIAS).forEach(function(group) {
      out[group] = base[GROUP_ALIAS[group]];
    });
    out['default-fill'] = base['external'];
    out['edge'] = base['edge'];
    out['edge-strong'] = base['edge-strong'];
    return out;
  }

  // ── Response adapter ───────────────────────────────────────────────────────

  // adapt converts the /code/graph/data flat JSON (nodes: [{id,label,kind,
  // file,line,language}], edges: [{source,target,kind}] — codegraph.go's
  // graphDataResponse) into cosmos's index-based arrays plus ID<->index maps.
  // Nodes are wrapped in a {data:{...}} envelope (mirroring system-graph.js's
  // adapt(), which mirrors the /graph/data Cytoscape shape) purely so
  // graph-core.js's typeOf()/buildLegend() — written against that shape — work
  // unchanged; this is the "mirror the docs adapter's output shape" contract
  // from docs/spec/code-graph.md checkpoint 5.
  function adapt(payload) {
    var rawNodes = (payload && payload.nodes) || [];
    var idToIndex = {};
    var indexToId = new Array(rawNodes.length);
    var nodes = new Array(rawNodes.length);
    rawNodes.forEach(function(n, i) {
      idToIndex[n.id] = i;
      indexToId[i] = n.id;
      nodes[i] = {
        data: {
          id: n.id, label: n.label, kind: n.kind, file: n.file, line: n.line,
          language: n.language, type: kindToGroup(n.kind)
        }
      };
    });

    var linkPairs = [];
    var linkClasses = []; // parallel to linkPairs' pair index — the edge's raw EdgeKind string
    ((payload && payload.edges) || []).forEach(function(e) {
      var s = idToIndex[e.source];
      var t = idToIndex[e.target];
      // Defensive: the server's edges are FK-consistent against its own nodes
      // dump (same query, same engine open), but a mismatched id would
      // corrupt the Float32Array below — skip rather than crash the mount.
      if (s === undefined || t === undefined) { return; }
      linkPairs.push(s, t);
      linkClasses.push(e.kind || '');
    });

    return {
      nodes: nodes,
      idToIndex: idToIndex,
      indexToId: indexToId,
      links: new Float32Array(linkPairs),
      linkClasses: linkClasses
    };
  }

  // ── Edge-kind styling (SC5) ─────────────────────────────────────────────────

  // linkStyle assigns per-link color/width from the edge's raw EdgeKind
  // string (types.go's EdgeKind — "contains", "calls", "imports", or one of
  // the remaining nine kinds). contains is the file/module containment
  // skeleton (SC5: "visually subordinate") — the majority of this repo's
  // ~55k edges, so it renders faint and thin to avoid a hairball; calls is
  // the primary relationship (full-strength edge-strong); imports is
  // secondary (edge-strong at reduced alpha); everything else (references,
  // writes, extends, implements, ...) is tertiary (plain edge color, midway
  // alpha/width between contains and imports).
  var CONTAINS_ALPHA = 0.18, CONTAINS_WIDTH = 0.5;
  var CALLS_ALPHA = 1, CALLS_WIDTH = 1.5;
  var IMPORTS_ALPHA = 0.65, IMPORTS_WIDTH = 1;
  var TERTIARY_ALPHA = 0.55, TERTIARY_WIDTH = 0.75;

  function linkStyle(kind, colors) {
    if (kind === 'contains') {
      return { color: window.GraphCore.hexToRGBA01(colors['edge'], CONTAINS_ALPHA), width: CONTAINS_WIDTH };
    }
    if (kind === 'calls') {
      return { color: window.GraphCore.hexToRGBA01(colors['edge-strong'], CALLS_ALPHA), width: CALLS_WIDTH };
    }
    if (kind === 'imports') {
      return { color: window.GraphCore.hexToRGBA01(colors['edge-strong'], IMPORTS_ALPHA), width: IMPORTS_WIDTH };
    }
    return { color: window.GraphCore.hexToRGBA01(colors['edge'], TERTIARY_ALPHA), width: TERTIARY_WIDTH };
  }

  // ── Meta / label resolvers ──────────────────────────────────────────────────

  // nodeMeta builds the plain-data object AtomicGraphUI's engine-neutral
  // showPreviewCard expects ({type, title, description, snippet}) — the
  // "Hover meta" contract (name, kind, file:line, language if handy) folded
  // into a single description line since the preview card has one slot for
  // it. type is the visual GROUP (not the raw kind) so the badge matches the
  // node's own dot color.
  function nodeMeta(adapted, index) {
    var raw = (adapted.nodes[index] && adapted.nodes[index].data) || {};
    var loc = raw.file ? (raw.file + (raw.line ? ':' + raw.line : '')) : '';
    var parts = [raw.kind, loc, raw.language].filter(Boolean);
    return {
      type: raw.type,
      title: raw.label || raw.id,
      description: parts.join(' · '),
      snippet: ''
    };
  }

  // labelText mirrors nodeMeta's title priority — the same text the hover
  // preview card and the DOM label overlay show for the same node.
  function labelText(adapted, id) {
    var n = adapted.nodes[adapted.idToIndex[id]];
    var raw = (n && n.data) || {};
    return raw.label || id;
  }

  // ── Data source ──────────────────────────────────────────────────────────

  // fetchData reads /code/graph/data (optionally scoped by ?member=) and
  // namespaces the cache key as "code:<member-or-local>:<fingerprint>" (SC4)
  // — a bare fingerprint would collide with the docs profile's own
  // /graph/data entries in the SAME IndexedDB store (graph-core.js's
  // LAYOUT_STORE is shared across profiles). Non-200 responses reject with
  // the server's own JSON error message (codegraph.go's graphErrorResponse)
  // so graph-core.js's mount() catch surfaces it verbatim in the pane — see
  // that file's showError() comment for why the catch reads e.message.
  function fetchData(member) {
    var qs = member ? ('?member=' + encodeURIComponent(member)) : '';
    return fetch('/code/graph/data' + qs).then(function(r) {
      return r.json().then(function(body) {
        if (!r.ok) {
          throw new Error((body && body.error) || ('code graph request failed: HTTP ' + r.status));
        }
        return { elements: body, cacheKey: 'code:' + (member || 'local') + ':' + body.fingerprint };
      });
    });
  }

  // ── Profile + public API ────────────────────────────────────────────────────

  // buildProfile closes the graph-core.js PROFILE contract over the member
  // prefix mount() was called with — the UI picker (CP6) will pass a
  // different member per switch; today's only caller (the gate harness, or a
  // future CP6 switcher) can omit it for the local/single-repo index.
  function buildProfile(member) {
    return {
      fetchData: function() { return fetchData(member); },
      adapt: adapt,
      colors: colors,
      linkStyle: linkStyle,
      nodeMeta: nodeMeta,
      labelText: labelText,
      onHover: function(meta, screenPos, container) {
        if (window.AtomicGraphUI) { window.AtomicGraphUI.showPreviewCard(meta, screenPos, container); }
      },
      onHoverOut: function() {
        if (window.AtomicGraphUI) { window.AtomicGraphUI.hidePreviewCard(); }
      },
      // onClick: the code-explorer node view (SC6) is checkpoint 6's job —
      // a no-op is the documented CP5 scope (docs/spec/code-graph.md).
      onClick: function() {},
      onTeardown: function() {
        if (window.AtomicGraphUI) { window.AtomicGraphUI.hidePreviewCard(); }
      }
    };
  }

  function mount(container, member) { window.GraphCore.mount(container, buildProfile(member)); }
  function teardown() { window.GraphCore.teardown(); }
  function retheme() { window.GraphCore.retheme(); }

  return {
    mount: mount, teardown: teardown, retheme: retheme,
    // Exported for parity with window.SystemGraph's shape — forwarded
    // verbatim from graph-core.js (shared engine, single live instance).
    computeLabelSet: window.GraphCore.computeLabelSet,
    LABEL_CAP: window.GraphCore.LABEL_CAP,
    LABEL_FADE_ZOOM_THRESHOLD: window.GraphCore.LABEL_FADE_ZOOM_THRESHOLD,
    // Exported for scripts/graph-gates.mjs (SC3/SC4 gate harness) — forwarded
    // from graph-core.js; see its debugState()/simRunning() comments.
    debugState: window.GraphCore.debugState,
    simRunning: window.GraphCore.simRunning
  };
}());
