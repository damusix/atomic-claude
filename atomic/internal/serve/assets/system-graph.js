// system-graph.js — FE3 Network View: mount lifecycle for the cosmos.gl-powered
// system graph. Served as a static asset (go:embed assets), loaded via
// <script src="/static/system-graph.js"> in layout.html, right after the
// vendored cosmos.gl bundle (window.Cosmos). See docs/design/cosmos-system-graph.md
// ("Code home" sub-decision) for why this subsystem lives here rather than
// inline in the template.
//
// CP2 scope: mount/teardown lifecycle, the JSON→cosmos data adapter, WebGL2
// detection, and the fresh-mount motion policy (sim runs to rest, then
// pauses). Position cache + drag reheat + URL view state land in a later
// checkpoint; so do the legend, hover/click, theme-flip, and label overlay.
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

    fetch('/graph/data')
      .then(function(r) { return r.json(); })
      .then(function(elems) {
        if (activeContainer !== container) { return; } // torn down mid-fetch

        var adapted = adapt(elems);
        var graph = new Cosmos.Graph(container, {
          // fitViewOnInit would fit to the random scatter below, before the
          // simulation has arranged anything — fit manually once it rests.
          fitViewOnInit: false,
          onSimulationEnd: function() {
            graph.pause();
            graph.fitView(undefined, undefined, false);
            clearLoading(mainPane);
          }
        });
        instance = graph;
        graph.setPointPositions(randomPositions(adapted.nodes.length, graph.config.spaceSize));
        graph.setLinks(adapted.links);
        graph.render();
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
