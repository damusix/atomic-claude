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
// kind→group palette (SC5: the 39 codeintel NodeKind values collapsed to ~8
// visual groups, each reading its own BRIGHT-band ramp shade off app.css's
// --cc-<group>/--ramp-<hue>-<n> vars — see GROUP_HUE below; the hue per group
// matches the docs profile's paired DUSKY type per docs/spec/code-graph.md's
// hue -> role table), edge-kind styling (contains subordinate, calls
// primary, imports secondary, everything else tertiary), and node
// meta/label resolution routed through AtomicGraphUI's engine-neutral
// preview-card hook (same hook the docs profile uses).
//
// Explorer click integration (SC6, this checkpoint) opens the existing
// code-explorer node view via window.AtomicCodeExplorer (layout.html); the
// Docs|Code switcher + member picker (SC7, this checkpoint) live in
// system-graph.js, which owns the shell-level /graph entry point this
// profile is mounted from. Everything else (mount/teardown/retheme
// lifecycle, WebGL2 gate, motion policy, layout cache, label overlay,
// legend, drag handling, debugState()/simRunning()) lives in graph-core.js
// and is forwarded verbatim below, mirroring window.SystemGraph's own public
// shape.
window.CodeGraph = (function() {

  // ── Kind → group taxonomy (SC5) ────────────────────────────────────────────

  // KIND_GROUPS maps every codeintel NodeKind string (atomic/internal/codeintel/types/types.go's
  // AllNodeKinds, 39 values) to one of 8 visual groups. A kind absent from
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
    'import': 'import-export', 'export': 'import-export', route: 'import-export',
    // package — synthesized shared hub for an external import specifier
    // (docs/spec/code-intel-package-nodes.md); groups with import/export/route
    // as the linkage-declaration cluster.
    'package': 'import-export'
  };

  function kindToGroup(kind) {
    return KIND_GROUPS[kind] || 'other';
  }

  // GROUP_HUE maps each of the 8 visual groups to its ramp hue
  // (docs/spec/code-graph.md hue -> role table) — the same hue as the docs
  // type sharing its role (gold: page/callable, slate: repo/module-file, …),
  // so the two graphs teach one hue vocabulary even though this view reads
  // its own BRIGHT band (app.css's --cc-<group> / --ramp-<hue>-<n>) directly
  // rather than aliasing through the docs profile's dusky --c-* colors.
  var GROUP_HUE = {
    'module-file': 'slate',
    'type': 'plum',
    callable: 'gold',
    value: 'moss',
    'sql-data': 'magenta',
    'sql-routine': 'terra',
    'import-export': 'cyan',
    other: 'gray'
  };

  // Shape per group, a second channel alongside hue (cosmos point-shape
  // indices — see POINT_SHAPE in utils/typeColors.ts). callable keeps the
  // circle: it is the bulk of any code graph, so it gets the quietest mark.
  var GROUP_SHAPE = {
    callable: 0,        // circle
    'module-file': 5,   // hexagon
    'type': 3,          // diamond
    value: 1,           // square
    'sql-data': 2,      // triangle
    'sql-routine': 4,   // pentagon
    'import-export': 6, // star
    other: 7            // cross
  };

  // colors() re-reads the --cc-*/--ramp-* CSS vars on every call (never
  // cached) so a theme flip picks up the new values — same contract
  // graph-core.js's applyStyling expects from every profile.colors().
  // atomicRampColors() is layout.html's shared ramp reader (sibling to
  // atomicCyTypeColors(), which the docs profile uses instead).
  function colors() {
    var style = getComputedStyle(document.documentElement);
    function v(name) { return style.getPropertyValue(name).trim(); }
    var ramps = atomicRampColors();
    var out = {};
    Object.keys(GROUP_HUE).forEach(function(group) {
      var hue = GROUP_HUE[group];
      // Vivid band, not bright: this canvas is near-black and the marks are a
      // few pixels each, where a muted fill has too little area to carry its
      // hue. The --cc-<group> var is no longer consulted for the node fill —
      // nodes and legend chip have to be the same color, and the chip is
      // drawn from this same entry.
      out[group + '-ramp'] = [1, 2, 3, 4, 5].map(function(n) { return ramps[hue + '-vivid-' + n]; });
      out[group] = out[group + '-ramp'][2];
    });
    out['default-fill'] = out['other'];
    out['default-ramp'] = out['other-ramp'];
    out['edge'] = v('--edge') || '#cabfae';
    out['edge-strong'] = v('--edge-strong') || '#b1a48f';
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
  // CONTAINS stays the faintest tier verbatim (graph-interactions brief,
  // item 4: "contains edges in the code view stay the faintest tier") — only
  // the three tiers above it move, alongside the --edge/--edge-strong
  // brightening in app.css.
  // 0.18 was tuned against the old flat --edge color, which is deliberately
  // brightened for contrast against the canvas. Tinted edges start from a node
  // fill instead, so the faintest tier needed the alpha back — and this tier is
  // not a rare accent: `contains` is ~40% of every code graph's edges, so at
  // "invisible" it removes the file-to-symbol structure from the picture.
  // Kind separates on alpha again, but across a deliberately narrow band —
  // five steps of 0.07 from 1 down to 0.72 (2026-08-16 user feedback). The
  // original tiers ran from 1 down to 0.18, which is what let the bottom tier
  // vanish; within this range every kind stays legible and the ordering is
  // still readable as emphasis. Width is uniform at 1px.
  var EDGE_ALPHA = {
    calls: 1,
    imports: 0.93,
    // Mutating and contract edges: fewer, and each one says more than a
    // reference does.
    writes: 0.86,
    implements: 0.86,
    references: 0.79,
    // The most numerous kind in any code graph, and the least surprising.
    contains: 0.72,
  };
  var EDGE_ALPHA_DEFAULT = 0.79;
  var EDGE_WIDTH = 1;

  // Every edge tints from its source node (graph-core's computeLinkStyling),
  // so the flat color here is only the fallback for a graph rendering without
  // point colors; alpha is what carries the kind.
  function linkStyle(kind, colors) {
    var alpha = EDGE_ALPHA[kind];
    if (alpha === undefined) { alpha = EDGE_ALPHA_DEFAULT; }
    return { color: window.GraphCore.hexToRGBA01(colors['edge-strong'], alpha), width: EDGE_WIDTH, tint: true };
  }

  // ── Meta / label resolvers ──────────────────────────────────────────────────

  // nodeMeta builds the plain-data object AtomicGraphUI's engine-neutral
  // showPreviewCard expects ({type, title, description, snippet}) — the
  // "Hover meta" contract (name, kind, file:line, language if handy) folded
  // into a single description line since the preview card has one slot for
  // it. type is the visual GROUP (not the raw kind) so the badge matches the
  // node's own dot color. file/line ride along (unused by the preview card)
  // for onClick's node-modal source pane below (SC6).
  function nodeMeta(adapted, index) {
    var raw = (adapted.nodes[index] && adapted.nodes[index].data) || {};
    var loc = raw.file ? (raw.file + (raw.line ? ':' + raw.line : '')) : '';
    var parts = [raw.kind, loc, raw.language].filter(Boolean);
    return {
      type: raw.type,
      title: raw.label || raw.id,
      description: parts.join(' · '),
      snippet: '',
      file: raw.file,
      line: raw.line
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
  // SHADE_CURVE remaps degree quintile → ramp shade for this view only
  // (graph-core.js's applyStyling reads profile.shadeCurve, default identity
  // [1,2,3,4,5]). Shade 1 of every hue is a pastel, deliberately reserved out
  // of this view's fill range — at code-graph density (~90% of nodes sit at
  // degree <=3, quintile 1, since real symbol-graph fan-out is leaf-heavy)
  // the identity curve rendered the whole view as a pastel wash instead of
  // the approved mock's dominant mid-ramp tones. [2,3,3,4,5]: quintile-1
  // leaves floor at shade 2 (the mock's dominant tone), quintiles 2-3 sit at
  // shade 3, and only the top two quintiles (real hubs) deepen further.
  var SHADE_CURVE = [2, 3, 3, 4, 5];

  function buildProfile(member) {
    return {
      fetchData: function() { return fetchData(member); },
      adapt: adapt,
      colors: colors,
      linkStyle: linkStyle,
      nodeMeta: nodeMeta,
      labelText: labelText,
      shadeCurve: SHADE_CURVE,
      onHover: function(meta, screenPos, container) {
        if (window.AtomicGraphUI) { window.AtomicGraphUI.showPreviewCard(meta, screenPos, container); }
      },
      onHoverOut: function() {
        if (window.AtomicGraphUI) { window.AtomicGraphUI.hidePreviewCard(); }
      },
      // onClick opens the existing code-explorer node view for this symbol
      // (SC6), member-aware — window.AtomicCodeExplorer.openNode is exposed
      // by layout.html's file-modal script alongside its own openModal(path,
      // anchor); see that function's comment for how it reuses the same
      // #code-modal machinery with the intel pane pointed at /code/node
      // instead of /code/file.
      onClick: function(id, meta) {
        if (window.AtomicCodeExplorer) { window.AtomicCodeExplorer.openNode(id, member, meta); }
      },
      shapeOf: function(_colors, group) {
        var shape = GROUP_SHAPE[group];
        return shape === undefined ? 0 : shape;
      },
      // Answers whether the modal THIS profile opens is up, so the core can
      // leave Escape to it (see the Escape branch in graph-core's mount()).
      // components/code-modal mirrors its open state onto the positioner as
      // `open`, since app.css shows the modal off that class.
      isModalOpen: function() {
        var el = document.getElementById('code-modal');
        return !!el && el.classList.contains('open');
      },
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
