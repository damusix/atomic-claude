// system-graph.js — FE3 Network View: mount lifecycle for the cosmos.gl-powered
// system graph. Served as a static asset (go:embed assets), loaded via
// <script src="/static/system-graph.js"> in layout.html, right after the
// vendored cosmos.gl bundle (window.Cosmos). See docs/design/cosmos-system-graph.md
// ("Code home" sub-decision) for why this subsystem lives here rather than
// inline in the template.
//
// CP2 landed mount/teardown lifecycle, the JSON→cosmos data adapter, WebGL2
// detection, and the fresh-mount motion policy (sim runs to rest, then
// pauses). CP3 adds the seed-and-pause position cache, bounded local drag
// reheat, and URL view-state read/write. The legend, hover/click, theme-flip,
// and label overlay remain for later checkpoints.
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
    ((elems && elems.edges) || []).forEach(function(e) {
      var s = idToIndex[e.data.source];
      var t = idToIndex[e.data.target];
      // The server already excludes dangling edges (TestGraphDataNoDanglingCodeFileEdge);
      // this is defense-in-depth against an edge whose endpoint didn't resolve.
      if (s === undefined || t === undefined) { return; }
      linkPairs.push(s, t);
    });

    return {
      nodes: nodes,
      idToIndex: idToIndex,
      indexToId: indexToId,
      links: new Float32Array(linkPairs)
    };
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
            clearLoading(mainPane);
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
            // userDriven excludes our own fitView/setZoomTransformByPointPositions
            // camera moves — cosmos.gl reports it directly, no timer-based guess needed.
            if (!recording || !userDriven) { return; }
            clearTimeout(viewTimer);
            viewTimer = setTimeout(function() { writeViewToURL(graph, container); }, VIEW_DEBOUNCE_MS);
          }
        });
        instance = graph;

        var seed = hit ? seedFromCache(adapted, cachePositions, graph.config.spaceSize)
          : randomPositions(adapted.nodes.length, graph.config.spaceSize);
        // dontRescale=true on a cache hit: the seeded coordinates are an exact
        // prior layout, not a fresh scatter — rescaling them would break the
        // "exact, zero-motion replay" contract. render(0) sets alpha to 0 so
        // the simulation stops after one frame with no motion, still firing
        // onSimulationEnd through the same shared handler above.
        graph.setPointPositions(seed, hit);
        graph.setLinks(adapted.links);
        graph.render(hit ? 0 : undefined);
      })
      .catch(function(e) {
        if (activeContainer !== container) { return; }
        container.dataset.systemMounted = ''; // allow a retry on re-navigation
        showError(mainPane, 'Could not render the system graph.');
        console.error('system-graph /graph/data:', e);
      });
  }

  // #btn-graph: in page view → open /graph; in graph view → back to the last
  // page (or landing). Delegated on document so it survives htmx
  // history-restore body swaps — a direct element listener would be lost
  // when #btn-graph is replaced.
  document.addEventListener('DOMContentLoaded', function() {
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

  return { mount: mount, teardown: teardown };
}());
