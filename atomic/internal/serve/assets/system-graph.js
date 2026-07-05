// system-graph.js — FE3 Network View: mount lifecycle for the cosmos.gl-powered
// system graph. Served as a static asset (go:embed assets), loaded via
// <script src="/static/system-graph.js"> in layout.html, right after the
// vendored cosmos.gl bundle (window.Cosmos). See docs/design/cosmos-system-graph.md
// ("Code home" sub-decision) for why this subsystem lives here rather than
// inline in the template.
//
// CP2 landed mount/teardown lifecycle, the JSON→cosmos data adapter, WebGL2
// detection, and the fresh-mount motion policy (sim runs to rest, then
// pauses). CP3 added the seed-and-pause position cache, bounded local drag
// reheat, and URL view-state read/write. CP4 adds the legend type-filter
// chips, hover/click wiring into AtomicGraphUI, theme-flip re-styling,
// degree-based sizing, OKF type coloring, and provenance/drift edge styling
// (relocated here from the shell's shared atomicCyStyle() — cosmos has no
// CSS selectors and no link dash-pattern API, so styling is computed and
// pushed per-point/per-link instead). CP5 adds the DOM label overlay: a
// screen-projected <div> per visible label, culled by the pure
// computeLabelSet() (exported below for scripts/test-system-graph-culling.cjs)
// and faded by zoom via a CSS class.
window.SystemGraph = (function() {

  // The live cosmos.gl instance, or null when no system graph is mounted.
  var instance = null;

  // The container the current mount (or in-flight /graph/data fetch) belongs
  // to. The fetch's .then compares against this before touching the DOM or
  // constructing an instance — a nav-out mid-fetch clears it, so a response
  // that resolves after teardown is discarded instead of mounting into a
  // detached container.
  var activeContainer = null;

  // Last /page/ URL loaded, restored when leaving the graph via #btn-graph.
  var currentPage = null;

  // Position cache — IndexedDB, keyed by the realm's X-Graph-Fingerprint (a
  // sha256 over the realm content, invalidated on any edit). Same DB/store
  // names as the removed Cytoscape-era cache so a user's existing entries are
  // found — then discarded by the format check below, not fed to the seed.
  var LAYOUT_DB = 'atomic-serve', LAYOUT_STORE = 'graph-layout';

  // CACHE_FORMAT_VERSION distinguishes this engine's seeded-position value
  // shape ({format, positions}) from the pre-swap Cytoscape shape
  // ({nodeID:{x,y}} directly, no `format` key at all) — any stored value
  // without a matching `format` fails the hit check in mount() below.
  var CACHE_FORMAT_VERSION = 2;

  // How long after the last user-driven zoom/pan before writing it to the URL.
  var VIEW_DEBOUNCE_MS = 250;

  // Bounded reheat energy for a node drag — enough for the dragged point's
  // immediate neighborhood to spring-adjust to its new position; too small a
  // value would leave the tick loop idle (see onDrag's comment), too large
  // would relayout more than the local neighborhood.
  var DRAG_REHEAT_ALPHA = 0.1;

  // SETTLE_SIMULATION_DECAY replaces cosmos's default (5000) so a fresh
  // mount reaches onSimulationEnd in a few seconds instead of tens of
  // seconds. GOTCHA verified against the unminified 3.1.0 bundle: the
  // config.d.ts prose reads "use smaller values if you want the simulation
  // to cool down slower" — backwards from the actual formula. Store.alphaDecay
  // is `1 - Q^(1/simulationDecay)` (Q = the 0.001 end threshold), which makes
  // simulationDecay exactly the number of ticks from alpha=1 to end,
  // independent of Q — SMALLER values settle FASTER, not slower.
  //
  // CORRECTION (Playwright-measured against this repo's own 358-node/331-edge
  // realm, not a synthetic fixture): decay=100 alone reaches onSimulationEnd
  // in ~2-3s, but 100 ticks is NOT enough for simulationLinkSpring to pull
  // connected components together against simulationRepulsion — the prior
  // claim that "layout shape is unaffected, only the tick budget shrinks" is
  // false. At decay=100 the fitView camera lands at zoom~0.22-0.35 (repulsion
  // has spread every component across most of spaceSize) and EVERY edge's
  // resulting on-screen length is smaller than its own endpoints' point
  // radius — edges are being drawn, just fully occluded by the two circles
  // they connect (pixel-sampled: rendered edge color, not background;
  // rendered edges are just shorter on screen than a point's own diameter).
  // Reference run (decay=5000, ~82s on this dataset) settles into a
  // recognizable hub-and-satellite layout (fitView zoom lands >1, i.e.
  // camera zooms IN because the converged bounding box is small relative to
  // the viewport). SETTLE_SIMULATION_GRAVITY and SETTLE_SIMULATION_REPULSION
  // below compensate at LOW tick counts: raising gravity (pulls every point
  // toward the shared center, compacting the overall spread) and lowering
  // repulsion (less push-apart per tick) let 200 ticks reach a layout
  // visually equivalent to the 5000-tick reference (Playwright-measured
  // structure ratio ~1.9-2.1 at decay=200 vs ~2.1 at decay=5000; screenshot-
  // confirmed same hub/satellite/singleton shape) in ~3.5-4.6s wall clock,
  // stable across reseeds (random initial scatter differs every mount).
  var SETTLE_SIMULATION_DECAY = 200;
  var SETTLE_SIMULATION_GRAVITY = 1.2;
  var SETTLE_SIMULATION_REPULSION = 0.4;

  // isWebGL2Available must run BEFORE constructing Cosmos.Graph — WebGL2
  // absence surfaces as getContext('webgl2') returning null, not a thrown
  // error, so the fetch-chain .catch below would never see it.
  function isWebGL2Available() {
    try {
      var c = document.createElement('canvas');
      return !!(c.getContext && c.getContext('webgl2'));
    } catch (e) {
      return false;
    }
  }

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

  function updateGraphBtnState(active) {
    var btn = document.getElementById('btn-graph');
    if (!btn) { return; }
    btn.setAttribute('aria-pressed', active ? 'true' : 'false');
    if (active) { btn.classList.add('btn-graph-active'); }
    else { btn.classList.remove('btn-graph-active'); }
  }

  // adapt converts the /graph/data cytoElements JSON into cosmos's index-based
  // arrays plus ID<->index maps — the one source of truth later event
  // handlers (hover, click, drag) will resolve node identity through.
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

  // ── Node meta + type resolution (shared by styling and AtomicGraphUI calls) ──

  // typeOf resolves a point index's OKF type, defaulting to 'page' — the same
  // fallback atomicCyStyle() uses for untyped nodes.
  function typeOf(adapted, index) {
    var n = adapted.nodes[index];
    return (n && n.data && n.data.type) || 'page';
  }

  // nodeMeta builds the plain-data object AtomicGraphUI's engine-neutral
  // showPreviewCard/openPageModal expect — cosmos has no per-point .data(), so
  // the adapter's own node list (indexed by point index) is the lookup.
  function nodeMeta(adapted, index) {
    var raw = (adapted.nodes[index] && adapted.nodes[index].data) || {};
    return { type: raw.type, title: raw.title, label: raw.label, description: raw.description, snippet: raw.snippet };
  }

  // ── Styling: degree-based sizing, OKF type coloring, provenance edges ──────

  // hexToRGBA01 parses a 6-digit "#rrggbb" string into cosmos's expected
  // [r, g, b, a] format — each channel 0..1, not 0..255.
  function hexToRGBA01(hex, alpha) {
    var h = String(hex).replace('#', '');
    return [
      parseInt(h.slice(0, 2), 16) / 255,
      parseInt(h.slice(2, 4), 16) / 255,
      parseInt(h.slice(4, 6), 16) / 255,
      alpha == null ? 1 : alpha
    ];
  }

  // DEGREE_CAP mirrors atomicCyStyle()'s degree-sizing input cap (16) — a
  // single mega-hub can't dwarf the rest of the graph.
  var DEGREE_CAP = 16;

  // computeDegrees counts each point's link count (both directions).
  function computeDegrees(adapted) {
    var deg = new Array(adapted.nodes.length).fill(0);
    var links = adapted.links;
    for (var i = 0; i < links.length; i += 2) {
      deg[links[i]] = Math.min(deg[links[i]] + 1, DEGREE_CAP);
      deg[links[i + 1]] = Math.min(deg[links[i + 1]] + 1, DEGREE_CAP);
    }
    return deg;
  }

  // MIN_POINT_SIZE/MAX_POINT_SIZE replace the old 1:1 port of
  // atomicCyStyle()'s 'mapData(deg, 0, 16, 16, 54)' — cosmos's GPU point
  // picking hit-tests against the exact rendered circle (findHoveredPoint
  // samples the same calculatePointSize() the vertex shader uses, verified
  // against the unminified 3.1.0 bundle), so growing these two constants
  // grows the click/hover target, not just the pixels. MIN roughly doubles
  // the old 16px floor (comfortable click target at degree 0); the additive
  // spread (38) is kept from the old mapping rather than scaled proportionally,
  // so a mega-hub grows in absolute size but not in the min-to-max RATIO —
  // it can't swallow the map the way a proportional bump would.
  var MIN_POINT_SIZE = 32, MAX_POINT_SIZE = 70;

  // sizeForDegree: linear map from degree range [0,DEGREE_CAP] to point-size
  // range [MIN_POINT_SIZE,MAX_POINT_SIZE].
  function sizeForDegree(deg) {
    return MIN_POINT_SIZE + (Math.min(deg, DEGREE_CAP) / DEGREE_CAP) * (MAX_POINT_SIZE - MIN_POINT_SIZE);
  }

  // Provenance edge colors. cosmos.gl links carry no dash-pattern API (checked
  // against the unminified 3.1.0 .d.ts: GraphConfigInterface has no line-style
  // option), so unlike the removed edge.fingerprint / edge.fingerprint.drift
  // Cytoscape selectors (dashed), the distinct-styling contract here is COLOR
  // (+ width) only — same hex values, same visual language, different renderer.
  var FINGERPRINT_COLOR = hexToRGBA01('#fab387', 1);
  var FINGERPRINT_DRIFT_COLOR = hexToRGBA01('#f38ba8', 1);
  var FINGERPRINT_WIDTH = 1.5;
  var FINGERPRINT_DRIFT_WIDTH = 2.5;

  // computeLinkStyling assigns per-link color/width from the edge's classes
  // string ("fingerprint" or "fingerprint drift" — graphoverlay.go's format).
  // wikilink gets the edge-strong color — mirrors the rail's atomicCyStyle()
  // edge.wikilink selector (dash pattern has no cosmos link-style API, so
  // color is the parity contract; see the design's Edge-kind colors row).
  // md-link and anything else gets the default themed edge color.
  function computeLinkStyling(adapted, colors) {
    var defaultColor = hexToRGBA01(colors['edge'], 1);
    var wikilinkColor = hexToRGBA01(colors['edge-strong'], 1);
    var n = adapted.linkClasses.length;
    var linkColors = new Float32Array(n * 4);
    var linkWidths = new Float32Array(n);
    for (var i = 0; i < n; i++) {
      var classes = adapted.linkClasses[i];
      var isFingerprint = classes.indexOf('fingerprint') !== -1;
      var isDrift = classes.indexOf('drift') !== -1;
      var isWikilink = classes.indexOf('wikilink') !== -1;
      var rgba = isFingerprint && isDrift ? FINGERPRINT_DRIFT_COLOR
        : isFingerprint ? FINGERPRINT_COLOR
        : isWikilink ? wikilinkColor
        : defaultColor;
      linkColors[i * 4] = rgba[0];
      linkColors[i * 4 + 1] = rgba[1];
      linkColors[i * 4 + 2] = rgba[2];
      linkColors[i * 4 + 3] = rgba[3];
      linkWidths[i] = isFingerprint && isDrift ? FINGERPRINT_DRIFT_WIDTH
        : isFingerprint ? FINGERPRINT_WIDTH
        : 1;
    }
    return { colors: linkColors, widths: linkWidths };
  }

  // computeNodeColors builds the per-point RGBA array from each node's OKF
  // type (atomicCyTypeColors() — single source of truth, also used by the
  // rail). A filtered-out type gets alpha 0 rather than being dropped from
  // the array: the point stays in the sim (no reflow), just invisible — the
  // hover/click guard (in mount()) is what actually excludes it from
  // interaction, since alpha-0 points still GPU-pick in cosmos.
  function computeNodeColors(adapted, colors, filteredTypes) {
    var n = adapted.nodes.length;
    var out = new Float32Array(n * 4);
    for (var i = 0; i < n; i++) {
      var type = typeOf(adapted, i);
      var rgba = hexToRGBA01(colors[type] || colors['default-fill'], filteredTypes[type] ? 0 : 1);
      out[i * 4] = rgba[0];
      out[i * 4 + 1] = rgba[1];
      out[i * 4 + 2] = rgba[2];
      out[i * 4 + 3] = rgba[3];
    }
    return out;
  }

  // computeNodeSizes is degree-only — unaffected by the legend filter (a
  // filtered point keeps its size, just goes transparent).
  function computeNodeSizes(degrees) {
    var out = new Float32Array(degrees.length);
    for (var i = 0; i < degrees.length; i++) { out[i] = sizeForDegree(degrees[i]); }
    return out;
  }

  // applyStyling recomputes and pushes point/link colors + point sizes from
  // the CURRENT atomicCyTypeColors() (re-read live so a theme flip picks up
  // the new CSS vars) and the current filteredTypes set. Called once at
  // mount, on every legend toggle, and from the theme-toggle's retheme() hook
  // below. create() (not render()) applies the pending buffers without
  // touching simulation state — a legend toggle or theme flip must never
  // reflow the layout.
  function applyStyling(graph, adapted, filteredTypes, degrees) {
    var colors = atomicCyTypeColors();
    graph.setPointColors(computeNodeColors(adapted, colors, filteredTypes));
    graph.setPointSizes(computeNodeSizes(degrees));
    var linkStyle = computeLinkStyling(adapted, colors);
    graph.setLinkColors(linkStyle.colors);
    graph.setLinkWidths(linkStyle.widths);
    graph.create();
  }

  // ── Legend: type-filter chip bar ───────────────────────────────────────────

  // buildLegend constructs the chip bar from the types present in the current
  // node set (mirrors the removed Cytoscape buildLegend's "types seen in
  // elements" approach). onToggle(type, hidden) is called with the NEW hidden
  // state so the caller can update filteredTypes and restyle.
  function buildLegend(adapted, colors, mainPane, onToggle) {
    var seenTypes = {};
    adapted.nodes.forEach(function(n) {
      var t = n.data && n.data.type;
      if (t) { seenTypes[t] = true; }
    });
    var types = Object.keys(seenTypes).sort();
    if (!types.length) { return; }

    var legend = document.createElement('div');
    legend.id = 'graph-legend';
    legend.className = 'graph-legend';

    types.forEach(function(type) {
      var color = colors[type] || colors['default-fill'];
      var chip = document.createElement('button');
      chip.className = 'graph-legend-chip graph-legend-chip-active';
      chip.type = 'button';
      chip.setAttribute('data-type', type);
      chip.setAttribute('aria-pressed', 'true');

      var swatch = document.createElement('span');
      swatch.className = 'graph-legend-swatch';
      swatch.style.background = color;
      var label = document.createElement('span');
      label.textContent = type;
      chip.appendChild(swatch);
      chip.appendChild(label);

      chip.addEventListener('click', function() {
        var hidden = chip.classList.contains('graph-legend-chip-active'); // active → clicking hides it
        chip.classList.toggle('graph-legend-chip-active', !hidden);
        chip.setAttribute('aria-pressed', hidden ? 'false' : 'true');
        onToggle(type, hidden);
      });
      legend.appendChild(chip);
    });
    mainPane.appendChild(legend);
  }

  // ── Label overlay: DOM projection + culling (CP5) ──────────────────────────

  // LABEL_CAP bounds the DOM label element count (SC5) as the realm grows —
  // implementer-tunable by editing this constant.
  var LABEL_CAP = 150;

  // LABEL_CANDIDATE_POOL bounds the per-event projection cost independent of
  // total node count: only the top-N-by-degree indices are tracked (via
  // trackPointPositionsByIndices in mount()) and re-projected on every
  // tick/zoom — never the whole node set. Ranked globally by degree, not by
  // current viewport, so a panned-into low-degree cluster may show fewer
  // than LABEL_CAP labels even with screen room to spare; accepted at
  // current realm scale. The hovered node is exempt from this pool — it is
  // always projected separately in updateLabels below.
  var LABEL_CANDIDATE_POOL = LABEL_CAP * 3;

  // LABEL_FADE_ZOOM_THRESHOLD is the zoom level below which labels fade out
  // (Obsidian-style): the realm-wide fit-on-open view starts faded, and
  // labels fade in as the user zooms toward individual nodes. Tunable.
  var LABEL_FADE_ZOOM_THRESHOLD = 1;

  // rankByDegree returns node indices sorted by degree descending, sliced to
  // LABEL_CANDIDATE_POOL. Degree is static for the session, so this runs
  // once at mount rather than on every label update.
  function rankByDegree(degrees) {
    var indices = degrees.map(function(_, i) { return i; });
    indices.sort(function(a, b) { return degrees[b] - degrees[a]; });
    return indices.slice(0, LABEL_CANDIDATE_POOL);
  }

  // computeLabelSet is the pure culling policy behind the DOM label overlay
  // (SC5) — no DOM, no cosmos instance, so it is callable directly from a
  // node test with synthetic fixtures (scripts/test-system-graph-culling.cjs).
  //
  //   candidates — [{ id, x, y, degree }]; x/y are already projected to the
  //     mount container's SCREEN-space coordinates by the caller (kept out
  //     of this function so it stays pure).
  //   viewport   — { width, height } of the mount container in screen pixels.
  //   zoom       — current cosmos zoom level (Graph#getZoomLevel()).
  //   opts       — { cap, fadeZoomThreshold, hoveredId }, all optional.
  //
  // Returns { render, faded } (both arrays of id):
  //   render — labels to keep mounted this tick: on-screen candidates ranked
  //     by degree descending, bounded by opts.cap, plus the hovered id
  //     unconditionally — even off-screen or over cap (SC5: "always shows
  //     regardless of zoom or cull set").
  //   faded  — the render ids below opts.fadeZoomThreshold; the hovered id
  //     is never faded.
  function computeLabelSet(candidates, viewport, zoom, opts) {
    opts = opts || {};
    var cap = opts.cap != null ? opts.cap : LABEL_CAP;
    var threshold = opts.fadeZoomThreshold != null ? opts.fadeZoomThreshold : LABEL_FADE_ZOOM_THRESHOLD;
    var hoveredId = opts.hoveredId != null ? opts.hoveredId : null;

    var onScreen = candidates.filter(function(c) {
      return c.id !== hoveredId
        && c.x >= 0 && c.x <= viewport.width
        && c.y >= 0 && c.y <= viewport.height;
    });
    onScreen.sort(function(a, b) { return b.degree - a.degree; });

    var render = onScreen.slice(0, cap).map(function(c) { return c.id; });
    if (hoveredId != null) { render.push(hoveredId); }

    var faded = zoom < threshold
      ? render.filter(function(id) { return id !== hoveredId; })
      : [];

    return { render: render, faded: faded };
  }

  // randomPositions scatters points across cosmos's coordinate space so the
  // repulsion force has room to spread them apart. cosmos does not randomize
  // positions itself — setPointPositions is the only source of point
  // coordinates, and an unset value renders zero points.
  function randomPositions(count, spaceSize) {
    var arr = new Float32Array(count * 2);
    for (var i = 0; i < count; i++) {
      arr[i * 2] = Math.random() * spaceSize;
      arr[i * 2 + 1] = Math.random() * spaceSize;
    }
    return arr;
  }

  // hasFullCoverage is the cache-hit gate: every node in the current node
  // set must have a finite position in positions (surplus IDs left over
  // from a since-shrunk node set are fine — they're just ignored). A
  // partial entry — e.g. an older single-node write, or a node set that has
  // since grown — fails this and falls through to the fresh-mount path
  // instead of seeding an incomplete replay.
  function hasFullCoverage(adapted, positions) {
    if (!positions) { return false; }
    for (var i = 0; i < adapted.nodes.length; i++) {
      var p = positions[adapted.indexToId[i]];
      if (!p || !isFinite(p.x) || !isFinite(p.y)) { return false; }
    }
    return true;
  }

  // seedFromCache builds the seeded Float32Array a cache-hit mount passes to
  // setPointPositions: cached x/y by node ID, remapped through adapted's
  // ID<->index maps (index order can't be assumed stable across opens). A
  // node absent from the cached map (shouldn't happen now that hit requires
  // hasFullCoverage above, kept as defense-in-depth) falls back to a random
  // position rather than (0,0).
  function seedFromCache(adapted, positions, spaceSize) {
    var arr = new Float32Array(adapted.nodes.length * 2);
    for (var i = 0; i < adapted.nodes.length; i++) {
      var p = positions[adapted.indexToId[i]];
      if (p) {
        arr[i * 2] = p.x;
        arr[i * 2 + 1] = p.y;
      } else {
        arr[i * 2] = Math.random() * spaceSize;
        arr[i * 2 + 1] = Math.random() * spaceSize;
      }
    }
    return arr;
  }

  // ── Position cache (IndexedDB) ────────────────────────────────────────────
  function idbOpen() {
    return new Promise(function(resolve, reject) {
      if (!window.indexedDB) { reject(new Error('no indexedDB')); return; }
      var req = indexedDB.open(LAYOUT_DB, 1);
      req.onupgradeneeded = function() {
        if (!req.result.objectStoreNames.contains(LAYOUT_STORE)) {
          req.result.createObjectStore(LAYOUT_STORE);
        }
      };
      req.onsuccess = function() { resolve(req.result); };
      req.onerror = function() { reject(req.error); };
    });
  }

  // loadCachedLayout returns whatever is stored under key verbatim (or null) —
  // the format-marker check lives in the caller, not here, since a miss vs. a
  // stale-format hit both fall through to the same "no seed" outcome but are
  // worth distinguishing in mount()'s reasoning.
  function loadCachedLayout(key) {
    if (!key) { return Promise.resolve(null); }
    return idbOpen().then(function(db) {
      return new Promise(function(resolve) {
        var rq = db.transaction(LAYOUT_STORE, 'readonly').objectStore(LAYOUT_STORE).get(key);
        rq.onsuccess = function() { resolve(rq.result || null); };
        rq.onerror = function() { resolve(null); };
      });
    }).catch(function() { return null; });
  }

  function saveCachedLayout(key, value) {
    if (!key) { return; }
    idbOpen().then(function(db) {
      db.transaction(LAYOUT_STORE, 'readwrite').objectStore(LAYOUT_STORE).put(value, key);
    }).catch(function() {});
  }

  // ── URL view-state (zoom + pan) ────────────────────────────────────────────
  // /graph?z=<zoom>&px=<panX>&py=<panY> — same three query keys as the removed
  // shell helpers, so an old bookmarked URL still parses and restores a view.
  // The values underneath aren't literally portable across the engine swap
  // (px/py is now a cosmos.gl space coordinate — the point centered in the
  // viewport — not cytoscape's screen-space pan offset; the two engines don't
  // share a coordinate system), but the shape and the restore-on-mount /
  // debounced-record-on-change behavior are preserved.
  function readViewFromURL() {
    var p = new URLSearchParams(window.location.search);
    var z = parseFloat(p.get('z')), px = parseFloat(p.get('px')), py = parseFloat(p.get('py'));
    if (isFinite(z) && isFinite(px) && isFinite(py)) { return { zoom: z, pan: { x: px, y: py } }; }
    return null;
  }

  // writeViewToURL reads the space-coordinate currently centered in the
  // viewport via screenToSpacePosition rather than the raw D3 zoom transform —
  // setZoomTransformByPointPositions (the only public camera-restore call) is
  // the write-side counterpart and takes a point-to-center, not a screen
  // offset, so save and restore must speak the same vocabulary.
  function writeViewToURL(graph, container) {
    if (window.location.pathname !== '/graph') { return; }
    var center = graph.screenToSpacePosition([container.clientWidth / 2, container.clientHeight / 2]);
    var qs = 'z=' + graph.getZoomLevel().toFixed(4) + '&px=' + center[0].toFixed(2) + '&py=' + center[1].toFixed(2);
    try { window.history.replaceState(window.history.state, '', '/graph?' + qs); } catch (e) {}
  }

  function clearLoading(mainPane) {
    var l = mainPane && mainPane.querySelector('.system-graph-loading');
    if (l) { l.remove(); }
  }

  // showError reuses the loading element as the error affordance — replacing
  // its text in place rather than removing it — mirroring the fetch .catch
  // pattern this replaces.
  function showError(mainPane, message) {
    var l = mainPane && mainPane.querySelector('.system-graph-loading');
    if (l) { l.textContent = message; }
  }

  // teardown destroys the live instance (releasing its GL context — browsers
  // cap live contexts, so a leaked instance per open/close cycle eventually
  // blacks out the view) and drops system mode. Safe with no live instance:
  // a mid-fetch nav-out clears activeContainer below, so the pending .then
  // bails before ever constructing one.
  function teardown() {
    activeContainer = null;
    if (instance) {
      try { instance.destroy(); } catch (e) {}
      instance = null;
    }
    document.body.classList.remove('mode-system');
    updateGraphBtnState(false);
    if (window.AtomicGraphUI) {
      window.AtomicGraphUI.hidePreviewCard();
      window.AtomicGraphUI.closePageModal();
    }
  }

  // mount is called via the htmx.onLoad delegation in layout.html whenever
  // the /graph fragment's [data-system-graph] container lands in #main-pane.
  function mount(container) {
    if (container.dataset.systemMounted === '1') { return; } // double-mount guard
    container.dataset.systemMounted = '1';
    activeContainer = container;

    var mainPane = document.getElementById('main-pane');
    document.body.classList.add('mode-system');
    updateGraphBtnState(true);

    if (!isWebGL2Available()) {
      showError(mainPane, 'Your browser does not support WebGL2, which the Network View requires.');
      return;
    }

    var fingerprint = null;

    fetch('/graph/data')
      .then(function(r) { fingerprint = r.headers.get('X-Graph-Fingerprint'); return r.json(); })
      .then(function(elems) {
        return loadCachedLayout(fingerprint).then(function(stored) { return { elems: elems, stored: stored }; });
      })
      .then(function(bundle) {
        if (activeContainer !== container) { return; } // torn down mid-fetch

        var adapted = adapt(bundle.elems);
        // A hit requires the format marker AND full node-set coverage — a
        // pre-swap entry (the old {id:{x,y}} shape) has no `format` key, and
        // a partial entry fails hasFullCoverage; both fall through to the
        // fresh-mount scatter below instead of seeding an incomplete replay.
        var formatOk = !!(bundle.stored && bundle.stored.format === CACHE_FORMAT_VERSION && bundle.stored.positions);
        var hit = formatOk && hasFullCoverage(adapted, bundle.stored.positions);
        var cachePositions = hit ? bundle.stored.positions : {};

        var draggedIndex = null;     // point index currently under the pointer, or null
        var pendingSaveIndex = null; // index due for a cache write once its post-drag cooldown reaches rest
        var mountFinished = false;   // gates the once-only fit/restore/clearLoading below from re-running after a drag's cooldown
        var recording = false;       // ignore our own programmatic camera moves; record only user-driven ones
        var viewTimer = null;
        var degrees = computeDegrees(adapted);
        var filteredTypes = {};      // type -> true while its legend chip is toggled off (alpha-0 + hover/click guard)
        var hoverIndex = null;       // point index the preview card is currently anchored to, or null
        var hoverSpacePos = null;    // that point's last-known SPACE position, refreshed by onPointMouseOver/onSimulationTick
        var labelCandidateIndices = rankByDegree(degrees); // top-N by degree, static for the session
        var labelPool = {};    // node id -> mounted <div class="graph-label"> element
        var labelLayer = null; // lazily-created overlay holding every label div, appended to `container`

        // reanchorHoverCard re-projects hoverSpacePos into screen coordinates and
        // re-renders the card there — spaceToScreenPosition is a pure
        // camera-matrix multiply, not a GPU readback, so calling it from the
        // tick/zoom handlers below stays clear of SC1's idle per-frame-work ban.
        function reanchorHoverCard() {
          if (hoverIndex == null || !hoverSpacePos || !window.AtomicGraphUI) { return; }
          if (filteredTypes[typeOf(adapted, hoverIndex)]) { return; }
          var screenPos = graph.spaceToScreenPosition(hoverSpacePos);
          window.AtomicGraphUI.showPreviewCard(nodeMeta(adapted, hoverIndex), { x: screenPos[0], y: screenPos[1] }, container);
        }

        function ensureLabelLayer() {
          if (labelLayer) { return labelLayer; }
          labelLayer = document.createElement('div');
          labelLayer.className = 'graph-label-layer';
          container.appendChild(labelLayer);
          return labelLayer;
        }

        // labelTextById mirrors nodeMeta's title/label priority — the same
        // text the hover preview card shows for the same node.
        function labelTextById(id) {
          var n = adapted.nodes[adapted.idToIndex[id]];
          var raw = (n && n.data) || {};
          return raw.title || raw.label || id;
        }

        // updateLabels re-projects the degree-ranked candidate pool (bounded
        // cost via the tracked-indices readback below — never the whole node
        // set) plus the hovered node's own position, runs the pure culling
        // policy, and reconciles the DOM label pool: divs are added/removed
        // only on cull-set membership changes, repositioned on every call.
        // Called only from tick/zoom/hover/legend events (SC1's idle clause) —
        // never from a self-scheduled loop.
        function updateLabels() {
          var viewport = { width: container.clientWidth, height: container.clientHeight };
          var zoom = graph.getZoomLevel();
          var candidates = [];
          graph.getTrackedPointPositionsMap().forEach(function(spacePos, idx) {
            if (filteredTypes[typeOf(adapted, idx)]) { return; } // legend-hidden: no floating label over an invisible point
            var screenPos = graph.spaceToScreenPosition(spacePos);
            candidates.push({ id: adapted.indexToId[idx], x: screenPos[0], y: screenPos[1], degree: degrees[idx] });
          });

          var hoveredId = null;
          if (hoverIndex != null && hoverSpacePos && !filteredTypes[typeOf(adapted, hoverIndex)]) {
            hoveredId = adapted.indexToId[hoverIndex];
            var hoverScreen = graph.spaceToScreenPosition(hoverSpacePos);
            // Pushed explicitly so a screen position is available even when
            // the hovered node isn't part of the degree-ranked candidate pool.
            candidates.push({ id: hoveredId, x: hoverScreen[0], y: hoverScreen[1], degree: degrees[hoverIndex] });
          }

          var result = computeLabelSet(candidates, viewport, zoom, {
            cap: LABEL_CAP, fadeZoomThreshold: LABEL_FADE_ZOOM_THRESHOLD, hoveredId: hoveredId
          });

          var posById = {};
          candidates.forEach(function(c) { posById[c.id] = c; });
          var fadedSet = {};
          result.faded.forEach(function(id) { fadedSet[id] = true; });
          var wanted = {};
          result.render.forEach(function(id) { wanted[id] = true; });

          Object.keys(labelPool).forEach(function(id) {
            if (!wanted[id]) { labelPool[id].remove(); delete labelPool[id]; }
          });

          result.render.forEach(function(id) {
            var pos = posById[id];
            if (!pos) { return; } // defensive — every render id came from candidates above
            var el = labelPool[id];
            if (!el) {
              el = document.createElement('div');
              el.className = 'graph-label';
              el.textContent = labelTextById(id);
              ensureLabelLayer().appendChild(el);
              labelPool[id] = el;
            }
            el.style.transform = 'translate(' + (pos.x + 6).toFixed(1) + 'px, ' + (pos.y + 4).toFixed(1) + 'px)';
            el.classList.toggle('graph-label-faded', !!fadedSet[id]);
          });
        }

        // onLegendToggle updates filteredTypes and restyles. The layout never
        // reflows: applyStyling only touches colors/sizes via
        // setPointColors/setPointSizes/setLinkColors/setLinkWidths + create(),
        // never setPointPositions.
        function onLegendToggle(type, hidden) {
          if (hidden) { filteredTypes[type] = true; } else { delete filteredTypes[type]; }
          if (hidden && hoverIndex != null && typeOf(adapted, hoverIndex) === type) {
            // F-5 carry-along: hiding the hovered node's own type would
            // otherwise leave the preview card frozen at its last position —
            // the point is now alpha-0 and guarded from hover/click, so its
            // card is unreachable until the type is shown again. Clear it.
            hoverIndex = null;
            hoverSpacePos = null;
            if (window.AtomicGraphUI) { window.AtomicGraphUI.hidePreviewCard(); }
          }
          applyStyling(graph, adapted, filteredTypes, degrees);
          updateLabels();
        }

        // saveFullSnapshot reads every current point's live position via
        // getPointPositions() — an event-driven, once-per-settle/release
        // call, never per animation frame — and writes the whole node set to
        // the cache. A full snapshot, not a single node, is what a later
        // open's hasFullCoverage hit gate requires; a partial write would
        // permanently block that open from ever seeding.
        function saveFullSnapshot() {
          var flat = graph.getPointPositions();
          var positions = {};
          for (var i = 0; i < adapted.nodes.length; i++) {
            positions[adapted.indexToId[i]] = { x: flat[i * 2], y: flat[i * 2 + 1] };
          }
          saveCachedLayout(fingerprint, { format: CACHE_FORMAT_VERSION, positions: positions });
        }

        // flushPendingSave writes the full snapshot once the dragged node's
        // cooldown reaches rest — not at raw pointer-release, since its
        // neighbors (unpinned during the drag) can still nudge positions
        // while cooling. Also called from onDragStart so a second drag
        // started before the first one's cooldown finishes doesn't lose the
        // write.
        function flushPendingSave() {
          if (pendingSaveIndex == null) { return; }
          saveFullSnapshot();
          pendingSaveIndex = null;
        }

        var graph = new Cosmos.Graph(container, {
          // fitViewOnInit would fit to the scatter/seed below, before the
          // simulation (or the cache replay) has settled — fit/restore
          // manually once onSimulationEnd fires.
          fitViewOnInit: false,
          enableDrag: true,
          simulationDecay: SETTLE_SIMULATION_DECAY,
          simulationGravity: SETTLE_SIMULATION_GRAVITY,
          simulationRepulsion: SETTLE_SIMULATION_REPULSION,
          onSimulationEnd: function() {
            graph.pause();
            if (pendingSaveIndex != null) {
              flushPendingSave(); // drag release: full snapshot, every cooldown
            } else if (!mountFinished && !hit) {
              // Fresh mount's first settle, cache-miss path only. A cache-hit
              // replay (render(0)) also fires onSimulationEnd, but `hit`
              // being true skips this branch — a cache-hit open must never
              // rewrite the cache (SC1).
              saveFullSnapshot();
            }
            graph.setPinnedPoints(null); // unconditional; a no-op before any drag has pinned anyone
            if (mountFinished) { return; } // a post-drag cooldown, not the initial mount — nothing else to do
            mountFinished = true;
            var saved = readViewFromURL();
            if (saved) {
              graph.setZoomTransformByPointPositions(new Float32Array([saved.pan.x, saved.pan.y]), 0, saved.zoom, 0, false);
            } else {
              graph.fitView(undefined, undefined, false);
            }
            buildLegend(adapted, atomicCyTypeColors(), mainPane, onLegendToggle);
            clearLoading(mainPane);
            updateLabels();
            recording = true;
          },
          // Bounded local reheat: pin everything except the dragged point and
          // its immediate neighbors (the design's "non-dragged neighborhood"),
          // so only that local cluster can respond to forces — the rest of
          // the graph is frozen for the whole drag + cooldown, matching SC1's
          // "leaves the rest of the graph in place." start(alpha) also resumes
          // the tick loop, which the drag's own per-frame position write rides
          // on — dragging while paused would otherwise never render.
          onDragStart: function(e) {
            var idx = e.subject && e.subject.index;
            if (idx == null) { return; }
            flushPendingSave();
            draggedIndex = idx;
            var keep = {};
            keep[idx] = true;
            graph.getNeighboringPointIndices(idx).forEach(function(n) { keep[n] = true; });
            var pin = [];
            for (var i = 0; i < adapted.nodes.length; i++) { if (!keep[i]) { pin.push(i); } }
            graph.setPinnedPoints(pin);
            graph.start(DRAG_REHEAT_ALPHA);
          },
          onDrag: function() {
            if (draggedIndex == null) { return; }
            // Reheating on every drag tick (not just dragstart) keeps alpha
            // topped up for as long as the pointer keeps moving — start() on
            // an already-running simulation resets alpha without refiring
            // onSimulationStart, so this can't fire the pause prematurely.
            graph.start(DRAG_REHEAT_ALPHA);
          },
          onDragEnd: function() {
            if (draggedIndex == null) { return; }
            // Saved once the cooldown reaches onSimulationEnd, not here — see
            // the Flow's release -> cools -> save ordering and flushPendingSave.
            pendingSaveIndex = draggedIndex;
            draggedIndex = null;
          },
          onZoom: function(e, userDriven) {
            // Re-anchor for ANY zoom/pan, not only user-driven ones — the
            // hovered node's screen position moves either way.
            reanchorHoverCard();
            updateLabels();
            // userDriven excludes our own fitView/setZoomTransformByPointPositions
            // camera moves — cosmos.gl reports it directly, no timer-based guess needed.
            if (!recording || !userDriven) { return; }
            clearTimeout(viewTimer);
            viewTimer = setTimeout(function() { writeViewToURL(graph, container); }, VIEW_DEBOUNCE_MS);
          },
          // hoverIndex/hoverSpacePos (declared above) track the currently
          // hovered node for reanchorHoverCard. The legend filter guard
          // excludes filtered-out points from hover/click — alpha-0 points
          // still GPU-pick in cosmos, so the guard has to live here.
          onPointMouseOver: function(index, pointPosition) {
            if (filteredTypes[typeOf(adapted, index)]) { return; }
            hoverIndex = index;
            hoverSpacePos = pointPosition;
            reanchorHoverCard();
            updateLabels();
          },
          onPointMouseOut: function() {
            hoverIndex = null;
            hoverSpacePos = null;
            if (window.AtomicGraphUI) { window.AtomicGraphUI.hidePreviewCard(); }
            updateLabels();
          },
          onPointClick: function(index, pointPosition) {
            if (filteredTypes[typeOf(adapted, index)] || !window.AtomicGraphUI) { return; }
            window.AtomicGraphUI.hidePreviewCard();
            window.AtomicGraphUI.openPageModal(adapted.indexToId[index], nodeMeta(adapted, index));
          },
          // onSimulationTick's hoveredIndex/pointPosition (populated only while
          // a point is under the pointer) is cosmos's already-computed
          // per-frame hover state — reusing it means "re-anchor while the sim
          // moves the hovered node" costs no extra readback, and it only fires
          // while the sim is actively running (never while idle, per SC1).
          onSimulationTick: function(alpha, tickHoveredIndex, tickPointPosition) {
            if (hoverIndex != null && tickHoveredIndex === hoverIndex && tickPointPosition) {
              hoverSpacePos = tickPointPosition;
              reanchorHoverCard();
            }
            updateLabels();
          }
        });
        instance = graph;
        instance.__atomicRetheme = function() { applyStyling(graph, adapted, filteredTypes, degrees); };
        // Bounded readback: only the degree-ranked candidate pool is tracked,
        // never the whole node set — see LABEL_CANDIDATE_POOL's comment.
        graph.trackPointPositionsByIndices(labelCandidateIndices);

        var seed = hit ? seedFromCache(adapted, cachePositions, graph.config.spaceSize)
          : randomPositions(adapted.nodes.length, graph.config.spaceSize);
        // dontRescale=true on a cache hit: the seeded coordinates are an exact
        // prior layout, not a fresh scatter — rescaling them would break the
        // "exact, zero-motion replay" contract. render(0) sets alpha to 0 so
        // the simulation stops after one frame with no motion, still firing
        // onSimulationEnd through the same shared handler above.
        graph.setPointPositions(seed, hit);
        graph.setLinks(adapted.links);
        // render() MUST run before applyStyling()'s create() call: a fresh
        // Cosmos.Graph starts store.pointsTextureSize at 0, and that field is
        // only ever computed inside render()->update(). Calling create()
        // (which applyStyling ends with) before the first render() makes
        // Points#updatePositions() bail out on that still-zero size WITHOUT
        // creating hoveredFbo, while still clearing the isPointPositionsUpdateNeeded
        // flag that would otherwise retry it — so hoveredFbo (used by every
        // hover-pick readback) is silently skipped forever. Verified via a
        // real-browser repro against the vendored bundle: reversing this
        // order reproduces "Cannot destructure property 'device' of 't' as
        // it is undefined" in findHoveredPoint on the very first hover, and
        // fixes it once render() runs first.
        graph.render(hit ? 0 : undefined);
        applyStyling(graph, adapted, filteredTypes, degrees);
      })
      .catch(function(e) {
        if (activeContainer !== container) { return; }
        container.dataset.systemMounted = ''; // allow a retry on re-navigation
        showError(mainPane, 'Could not render the system graph.');
        console.error('system-graph /graph/data:', e);
      });
  }

  // retheme re-pushes point/link colors from the live cosmos.gl instance's
  // stored adapter/filter state — called by the shell's theme-toggle handler.
  // A no-op with no live instance (page view, or mid-fetch before the
  // instance is constructed).
  function retheme() {
    if (instance && instance.__atomicRetheme) { instance.__atomicRetheme(); }
  }

  // #btn-graph: in page view → open /graph; in graph view → back to the last
  // page (or landing). Delegated on document so it survives htmx
  // history-restore body swaps — a direct element listener would be lost
  // when #btn-graph is replaced.
  document.addEventListener('DOMContentLoaded', function() {
    // Modal dismiss wiring (scrim backdrop, corner ×, Close button, Esc). The
    // old shell called this from its own DOMContentLoaded block; CP2's
    // mount-body trim removed that whole block along with it, leaving the
    // page modal with no way to close once click→openPageModal (below and in
    // mount()) opens one. Restored here as part of this checkpoint's click
    // wiring; wireDismiss() is itself idempotent.
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
    // Exported for scripts/test-system-graph-culling.cjs — the pure culling
    // policy behind SC5's label overlay, plus its tunable defaults.
    computeLabelSet: computeLabelSet,
    LABEL_CAP: LABEL_CAP,
    LABEL_FADE_ZOOM_THRESHOLD: LABEL_FADE_ZOOM_THRESHOLD
  };
}());
