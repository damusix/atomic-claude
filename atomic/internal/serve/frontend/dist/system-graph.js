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
  // DEFAULT_WIDTH (graph-interactions brief, item 4): 1 -> 1.25 alongside the
  // --edge/--edge-strong brightening in app.css — plain md-link/wikilink
  // edges were the faintest tier here (full alpha already, so only width and
  // the CSS var itself had room to move).
  var DEFAULT_WIDTH = 1.25;

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
      : DEFAULT_WIDTH;
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

  // ── Graph pane orchestration (code-graph checkpoint 6) ─────────────────────
  // Docs|Code switcher, realm member picker, and their URL state (`view`,
  // `member` — alongside graph-core.js's existing `z`/`px`/`py` camera
  // state). Lives here rather than in code-graph.js: this file already owns
  // the shell-level graph-mode entry point (#btn-graph below) and the only
  // other consumer (CodeGraph) has no shell/URL concerns of its own.

  function currentView() {
    return new URLSearchParams(window.location.search).get('view') === 'code' ? 'code' : 'docs';
  }
  function currentMember() {
    return new URLSearchParams(window.location.search).get('member') || '';
  }

  // setViewParams writes view/member into the URL. z/px/py are dropped: a
  // different graph (or a different repo's graph) invalidates whatever
  // camera position was recorded for the last one — the freshly-mounted
  // profile's own fitView + the next user pan/zoom (graph-core.js's
  // writeViewToURL, which now merges rather than replaces — see its comment)
  // repopulate them.
  function setViewParams(view, member) {
    var params = new URLSearchParams(window.location.search);
    if (view === 'code') { params.set('view', 'code'); } else { params.delete('view'); }
    if (member) { params.set('member', member); } else { params.delete('member'); }
    params.delete('z'); params.delete('px'); params.delete('py');
    var qs = params.toString();
    try { window.history.replaceState(window.history.state, '', '/graph' + (qs ? '?' + qs : '')); } catch (e) {}
  }

  function escapeAttr(s) {
    return String(s).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
  }

  function graphPaneControlsHTML(view) {
    return '<div class="graph-pane-controls" id="graph-pane-controls">' +
      '<span class="search-toggle graph-view-switch" role="group" aria-label="Graph view">' +
        '<button type="button" class="toggle-btn' + (view === 'docs' ? ' toggle-active' : '') + '" data-graph-view="docs" aria-pressed="' + (view === 'docs') + '">Docs</button>' +
        '<button type="button" class="toggle-btn' + (view === 'code' ? ' toggle-active' : '') + '" data-graph-view="code" aria-pressed="' + (view === 'code') + '">Code</button>' +
      '</span>' +
      '<span id="graph-member-picker-slot"></span>' +
    '</div>';
  }

  // renderMemberPickerOptions fills the picker slot with a <select> built
  // from an already-fetched members list (mountCodeView below) — only when
  // more than one member exists (SC7: single-repo mode renders no picker;
  // repo scope always returns exactly one member — see code_graph_members.go).
  function renderMemberPickerOptions(members, selectedMember) {
    var slot = document.getElementById('graph-member-picker-slot');
    if (!slot) { return; }
    var html = '<select id="graph-member-select" class="graph-member-select" aria-label="Code member">';
    members.forEach(function(m) {
      var label = (m.prefix || '(local)') + (m.indexed ? '' : ' — not indexed');
      html += '<option value="' + escapeAttr(m.prefix) + '"' + (m.prefix === selectedMember ? ' selected' : '') + '>' + escapeAttr(label) + '</option>';
    });
    html += '</select>';
    slot.innerHTML = html;
  }

  // mountCodeView resolves which member to mount and fills the picker from
  // ONE /code/graph/members fetch, then calls CodeGraph.mount.
  //
  // Realm scope (more than one member) has no meaningful "empty member"
  // fallback: codegraph.go's own default (no ?member= param) falls through
  // to a root-level index that never exists in a realm with no code
  // committed at the realm root itself. So a code-view entry with no member
  // in the URL — or a member the server doesn't recognize — picks the FIRST
  // discovered member instead of showing a spurious not-indexed error before
  // the user has touched anything; the URL is updated to match (so a
  // subsequent reload/share restores that same member, not just "code
  // view"). Single-repo scope (<=1 member) is unaffected — member stays ''
  // (local index) either way, and the picker slot is left empty (SC7).
  //
  // A fetch failure (network hiccup, not a "not indexed" response — that's a
  // 200 with indexed:false) falls back to mounting with whatever member the
  // URL already named, same as if this fetch had never run.
  function mountCodeView(container, member) {
    fetch('/code/graph/members').then(function(r) { return r.json(); }).then(function(data) {
      if (document.getElementById(container.id) !== container) { return; } // pane re-rendered mid-fetch
      var members = (data && data.members) || [];
      var resolved = member;
      if (members.length > 1 && !members.some(function(m) { return m.prefix === member; })) {
        resolved = members[0].prefix;
        setViewParams('code', resolved);
      }
      window.CodeGraph.mount(container, resolved || undefined);
      if (members.length > 1) { renderMemberPickerOptions(members, resolved); }
    }).catch(function() {
      if (document.getElementById(container.id) === container) {
        window.CodeGraph.mount(container, member || undefined);
      }
    });
  }

  // renderGraphPane fully rebuilds #main-pane's content for the given view —
  // switcher controls + a FRESH mount container + loading marker — then
  // mounts the matching profile. A full rebuild (rather than reusing the
  // prior container) is what makes switching safe: every per-mount DOM
  // artifact graph-core.js creates (the legend, the label layer, the loading
  // marker) is appended as a mainPane/container child, so wiping mainPane's
  // innerHTML discards all of it in one step — no separate cleanup list to
  // keep in sync as future checkpoints add more per-mount DOM.
  function renderGraphPane(view, member) {
    var mainPane = document.getElementById('main-pane');
    if (!mainPane) { return; }
    var containerId = view === 'code' ? 'code-cy' : 'system-cy';
    var containerAttr = view === 'code' ? 'data-code-graph' : 'data-system-graph';
    mainPane.innerHTML = graphPaneControlsHTML(view) +
      '<div id="' + containerId + '" ' + containerAttr + '></div>' +
      '<p class="loading system-graph-loading">Laying out graph…</p>';

    var container = document.getElementById(containerId);
    if (view === 'code') {
      mountCodeView(container, member);
    } else {
      window.SystemGraph.mount(container);
    }
  }

  // enterGraphMode reads the current URL's view/member and renders the graph
  // pane accordingly. Called once per /graph entry — whether via #btn-graph
  // (always plain /graph, so defaults to docs view) or a document load /
  // reload of a /graph?view=code&member=<prefix> URL: the browser keeps that
  // query string in window.location even though the htmx fragment fetch that
  // populates #main-pane on load always requests bare /graph (serve.go's
  // systemGraphFragmentHTML is view-agnostic — its DOM is immediately
  // replaced by renderGraphPane below regardless of which view it names).
  function enterGraphMode() {
    var params = new URLSearchParams(window.location.search);
    var view = params.get('view') === 'code' ? 'code' : 'docs';
    var member = params.get('member') || '';
    renderGraphPane(view, member);
  }

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

    // Docs|Code switcher (checkpoint 6): swap the mounted profile in place —
    // still in graph mode, just a different graph — and record the choice
    // in the URL.
    document.addEventListener('click', function(e) {
      var btn = e.target.closest && e.target.closest('[data-graph-view]');
      if (!btn) { return; }
      var view = btn.getAttribute('data-graph-view');
      if (view === currentView()) { return; }
      var member = view === 'code' ? currentMember() : '';
      setViewParams(view, member);
      renderGraphPane(view, member);
    });

    // Realm member picker (checkpoint 6): switching members re-mounts the
    // code view against the new member's index.
    document.addEventListener('change', function(e) {
      var sel = e.target.closest && e.target.closest('#graph-member-select');
      if (!sel) { return; }
      setViewParams('code', sel.value);
      renderGraphPane('code', sel.value);
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
    // enterGraphMode — the checkpoint 6 /graph entry point layout.html's FE3
    // htmx.onLoad delegation calls instead of mount() directly, so every
    // /graph arrival goes through the Docs|Code + URL-state dispatch above.
    enterGraphMode: enterGraphMode,
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
