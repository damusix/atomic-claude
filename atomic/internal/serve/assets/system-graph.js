// system-graph.js — docs Network View: the profile that wires the shared
// cosmos.gl engine (graph-core.js, loaded first — see layout.html) to the
// docs realm's /graph/data endpoint. Served as a static asset (go:embed
// assets), loaded via <script src="/static/system-graph.js"> in layout.html,
// right after graph-core.js and the vendored cosmos.gl bundle
// (window.Cosmos). See docs/design/cosmos-system-graph.md ("Code home"
// sub-decision) for why this subsystem lives here rather than inline in the
// template, and docs/spec/code-graph.md checkpoint 4 for the core/profile
// split this file is one half of.
//
// Owns: the /graph/data fetch + fingerprint-as-cache-key, the cytoElements
// JSON → cosmos adapter, the OKF type→color palette and provenance/drift
// edge-kind styling (fingerprint/drift/wikilink — graphoverlay.go's format),
// node meta/label resolution, hover/click wiring into AtomicGraphUI, and the
// docs-shell-specific navigation glue (#btn-graph toggle between page view
// and /graph, current-page tracking, modal dismiss wiring) — none of which
// graph-core.js knows about. Everything else (mount/teardown/retheme
// lifecycle, WebGL2 gate, motion policy, layout cache, label overlay,
// legend, drag handling, debugState()/simRunning()) lives in graph-core.js
// and is forwarded verbatim below.
window.SystemGraph = (function() {

  // Last /page/ URL loaded, restored when leaving the graph via #btn-graph.
  var currentPage = null;

  // The brand link is the only place {{.LandingURL}} is already rendered into
  // the page; reading it here avoids re-plumbing that template value into a
  // static asset (which html/template never processes).
  function landingURL() {
    var brand = document.querySelector('.brand');
    return (brand && brand.getAttribute('href')) || '/';
  }

  function inSystemMode() {
    return document.body.classList.contains('mode-system');
  }

  // adapt converts the /graph/data cytoElements JSON into cosmos's index-based
  // arrays plus ID<->index maps — the one source of truth later event
  // handlers (hover, click, drag) will resolve node identity through. This is
  // the profile's "response adapter" per graph-core.js's mount(container,
  // profile) contract.
  function adapt(elems) {
    var nodes = (elems && elems.nodes) || [];
    var idToIndex = {};
    var indexToId = new Array(nodes.length);
    nodes.forEach(function(n, i) {
      idToIndex[n.data.id] = i;
      indexToId[i] = n.data.id;
    });

    var linkPairs = [];
    var linkClasses = []; // parallel to linkPairs' pair index — e.g. "fingerprint" or "fingerprint drift"
    ((elems && elems.edges) || []).forEach(function(e) {
      var s = idToIndex[e.data.source];
      var t = idToIndex[e.data.target];
      // The server already excludes dangling edges (TestGraphDataNoDanglingCodeFileEdge);
      // this is defense-in-depth against an edge whose endpoint didn't resolve.
      if (s === undefined || t === undefined) { return; }
      linkPairs.push(s, t);
      linkClasses.push(e.classes || '');
    });

    return {
      nodes: nodes,
      idToIndex: idToIndex,
      indexToId: indexToId,
      links: new Float32Array(linkPairs),
      linkClasses: linkClasses
    };
  }

  // Provenance edge colors. cosmos.gl links carry no dash-pattern API (checked
  // against the unminified 3.1.0 .d.ts: GraphConfigInterface has no line-style
  // option), so unlike the removed edge.fingerprint / edge.fingerprint.drift
  // Cytoscape selectors (dashed), the distinct-styling contract here is COLOR
  // (+ width) only — same hex values, same visual language, different renderer.
  var FINGERPRINT_COLOR = window.GraphCore.hexToRGBA01('#fab387', 1);
  var FINGERPRINT_DRIFT_COLOR = window.GraphCore.hexToRGBA01('#f38ba8', 1);
  var FINGERPRINT_WIDTH = 1.5;
  var FINGERPRINT_DRIFT_WIDTH = 2.5;

  // linkStyle assigns per-link color/width from the edge's classes string
  // ("fingerprint" or "fingerprint drift" — graphoverlay.go's format).
  // wikilink gets the edge-strong color — mirrors the rail's atomicCyStyle()
  // edge.wikilink selector (dash pattern has no cosmos link-style API, so
  // color is the parity contract; see the design's Edge-kind colors row).
  // md-link and anything else gets the default themed edge color.
  function linkStyle(classes, colors) {
    var isFingerprint = classes.indexOf('fingerprint') !== -1;
    var isDrift = classes.indexOf('drift') !== -1;
    var isWikilink = classes.indexOf('wikilink') !== -1;
    var color = isFingerprint && isDrift ? FINGERPRINT_DRIFT_COLOR
      : isFingerprint ? FINGERPRINT_COLOR
      : isWikilink ? window.GraphCore.hexToRGBA01(colors['edge-strong'], 1)
      : window.GraphCore.hexToRGBA01(colors['edge'], 1);
    var width = isFingerprint && isDrift ? FINGERPRINT_DRIFT_WIDTH
      : isFingerprint ? FINGERPRINT_WIDTH
      : 1;
    return { color: color, width: width };
  }

  // nodeMeta builds the plain-data object AtomicGraphUI's engine-neutral
  // showPreviewCard/openPageModal expect — cosmos has no per-point .data(), so
  // the adapter's own node list (indexed by point index) is the lookup.
  function nodeMeta(adapted, index) {
    var raw = (adapted.nodes[index] && adapted.nodes[index].data) || {};
    return { type: raw.type, title: raw.title, label: raw.label, description: raw.description, snippet: raw.snippet };
  }

  // labelText mirrors nodeMeta's title/label priority — the same text the
  // hover preview card shows for the same node.
  function labelText(adapted, id) {
    var n = adapted.nodes[adapted.idToIndex[id]];
    var raw = (n && n.data) || {};
    return raw.title || raw.label || id;
  }

  var docsProfile = {
    // fetchData reads the fingerprint header BEFORE parsing the body (order
    // matters: r.json() consumes the response) and uses it verbatim as the
    // layout cache key — no namespace prefix, so a user's pre-existing cache
    // entries (written under the same bare fingerprint by the removed
    // Cytoscape-era system graph) are still found.
    fetchData: function() {
      var fingerprint = null;
      return fetch('/graph/data')
        .then(function(r) { fingerprint = r.headers.get('X-Graph-Fingerprint'); return r.json(); })
        .then(function(elems) { return { elements: elems, cacheKey: fingerprint }; });
    },
    adapt: adapt,
    colors: function() { return atomicCyTypeColors(); },
    linkStyle: linkStyle,
    nodeMeta: nodeMeta,
    labelText: labelText,
    onHover: function(meta, screenPos, container) {
      if (window.AtomicGraphUI) { window.AtomicGraphUI.showPreviewCard(meta, screenPos, container); }
    },
    onHoverOut: function() {
      if (window.AtomicGraphUI) { window.AtomicGraphUI.hidePreviewCard(); }
    },
    onClick: function(id, meta) {
      if (!window.AtomicGraphUI) { return; }
      window.AtomicGraphUI.hidePreviewCard();
      window.AtomicGraphUI.openPageModal(id, meta);
    },
    // onTeardown dismisses whatever hover/click UI this profile opened —
    // graph-core.js's teardown() fires this (if defined) instead of
    // hardcoding AtomicGraphUI, a docs-shell-specific global, into the
    // view-agnostic core.
    onTeardown: function() {
      if (!window.AtomicGraphUI) { return; }
      window.AtomicGraphUI.hidePreviewCard();
      window.AtomicGraphUI.closePageModal();
    }
  };

  function mount(container) { window.GraphCore.mount(container, docsProfile); }
  function teardown() { window.GraphCore.teardown(); }
  function retheme() { window.GraphCore.retheme(); }

  // #btn-graph: in page view → open /graph; in graph view → back to the last
  // page (or landing). Delegated on document so it survives htmx
  // history-restore body swaps — a direct element listener would be lost
  // when #btn-graph is replaced.
  document.addEventListener('DOMContentLoaded', function() {
    // Modal dismiss wiring (scrim backdrop, corner ×, Close button, Esc). The
    // old shell called this from its own DOMContentLoaded block; CP2's
    // mount-body trim removed that whole block along with it, leaving the
    // page modal with no way to close once click→openPageModal (in
    // docsProfile.onClick above) opens one. Restored here as part of that
    // checkpoint's click wiring; wireDismiss() is itself idempotent.
    if (window.AtomicGraphUI) { window.AtomicGraphUI.wireDismiss(); }

    document.addEventListener('click', function(e) {
      if (!e.target.closest || !e.target.closest('#btn-graph')) { return; }
      var target = inSystemMode() ? (currentPage || landingURL()) : '/graph';
      htmx.ajax('GET', target, { target: '#main-pane', swap: 'innerHTML' });
      if (window.history && window.history.pushState) { window.history.pushState(null, '', target); }
    });
  });

  // Track the current page + tear down system mode whenever #main-pane
  // settles with non-graph content (nav click, node "open full page", Back
  // to a page, etc.). htmx 4 colon-format events carry detail.ctx.
  document.addEventListener('htmx:after:swap', function(evt) {
    var ctx = evt.detail && evt.detail.ctx;
    var tgt = ctx && ctx.target;
    if (!tgt || tgt.id !== 'main-pane') { return; }
    var path = ctx.request && ctx.request.action;
    if (path && path.indexOf('/page/') === 0) { currentPage = path; }
    if (!tgt.querySelector('[data-system-graph]') && inSystemMode()) {
      teardown();
    }
  });

  return {
    mount: mount, teardown: teardown, retheme: retheme,
    // Exported for scripts/test-system-graph-culling.cjs — forwarded from
    // graph-core.js, which now owns the pure culling policy behind SC5's
    // label overlay (see that file's own comment on why it moved).
    computeLabelSet: window.GraphCore.computeLabelSet,
    LABEL_CAP: window.GraphCore.LABEL_CAP,
    LABEL_FADE_ZOOM_THRESHOLD: window.GraphCore.LABEL_FADE_ZOOM_THRESHOLD,
    // Exported for scripts/graph-gates.mjs (SC3 gate harness) — forwarded
    // from graph-core.js; see its debugState()/simRunning() comments.
    debugState: window.GraphCore.debugState,
    simRunning: window.GraphCore.simRunning
  };
}());
