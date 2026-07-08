// graph-core.js — view-agnostic cosmos.gl engine extracted from
// system-graph.js (code-graph checkpoint 4). Served as a static asset
// (go:embed assets), loaded via <script src="/static/graph-core.js"> in
// layout.html, after the vendored cosmos.gl bundle (window.Cosmos) and
// BEFORE the per-view consumer (system-graph.js today; a future code-graph.js
// profile per docs/spec/code-graph.md checkpoint 5). See
// docs/design/cosmos-system-graph.md ("Code home" sub-decision) for why this
// subsystem lives here rather than inline in the template.
//
// Owns: mount/teardown/retheme lifecycle, WebGL2 detect-and-message, the
// tuned motion policy (settle-then-pause constants, drag reheat with
// DRAG_REPULSION_BOOST, styling-flush ordering), the IndexedDB layout cache,
// the DOM label overlay (degree-capped, zoom-faded, culled by the pure
// computeLabelSet() — exported below for scripts/test-system-graph-culling.cjs),
// the legend chip machinery, drag handling (including the
// graph.store.draggingPointIndex workaround), and the debugState()/
// simRunning() test accessors consumed by scripts/graph-gates.mjs (SC3 gate
// harness) — neither participates in the production mount/render/drag/cache
// flow above.
//
// Delegates to a per-view PROFILE object (passed to mount()): data fetch +
// response adapter, palette/type taxonomy (colors()/linkStyle()), cache key,
// label/meta resolvers (labelText()/nodeMeta()), hover/click hooks
// (onHover()/onHoverOut()/onClick()), and an optional onTeardown() cleanup
// hook (fired by teardown() below if the profile defines one — e.g. to
// dismiss whatever hover/click UI the profile opened). system-graph.js (the
// docs Network View, over /graph/data) and code-graph.js (over
// /code/graph/data, checkpoint 5) are the two current profiles; both run the
// core's drag physics constants verbatim (docs/spec/code-graph.md checkpoint
// 5 follow-up: an empirical sweep found the constants were never the
// bottleneck for a forced drag overlap at code-graph scale — the gate's own
// drag-target pick was, see scripts/graph-gates.mjs's gate 3 node-pick
// comment). A per-profile override is not exposed — re-add when a real
// profile actually needs one.
window.GraphCore = (function() {

  // The live cosmos.gl instance, or null when no graph is mounted.
  var instance = null;

  // The container the current mount (or in-flight data fetch) belongs to.
  // The fetch's .then compares against this before touching the DOM or
  // constructing an instance — a nav-out mid-fetch clears it, so a response
  // that resolves after teardown is discarded instead of mounting into a
  // detached container.
  var activeContainer = null;

  // The profile passed to the current (or in-flight) mount() — teardown()
  // reads this to fire the profile's optional onTeardown() hook without
  // hardcoding a docs-specific global (see teardown()'s comment).
  var activeProfile = null;

  // Position cache — IndexedDB, keyed by the profile-supplied cache key (for
  // the docs profile: the realm's X-Graph-Fingerprint, a sha256 over the
  // realm content, invalidated on any edit). Same DB/store names as the
  // removed Cytoscape-era cache so a user's existing entries are found — then
  // discarded by the format check below, not fed to the seed.
  var LAYOUT_DB = 'atomic-serve', LAYOUT_STORE = 'graph-layout';

  // CACHE_FORMAT_VERSION distinguishes this engine's seeded-position value
  // shape ({format, positions}) from the pre-swap Cytoscape shape
  // ({nodeID:{x,y}} directly, no `format` key at all) — any stored value
  // without a matching `format` fails the hit check in mount() below.
  var CACHE_FORMAT_VERSION = 2;

  // How long after the last user-driven zoom/pan before writing it to the URL.
  var VIEW_DEBOUNCE_MS = 250;

  // Reheat energy for a node drag, applied to the LIVE simulation with no
  // pinning (2026-07-05 spec amendment — dragging is a live reheat, not a
  // pin-the-rest-of-the-graph containment). Every force scales by this
  // alpha, so it mainly exists to keep the tick loop running for the
  // duration of the drag — see onDrag's comment — and to bound the overall
  // energy budget; too large would perturb nodes well outside the drag's
  // neighborhood on its own.
  //
  // A first pass tuned only this alpha (0.02-0.1, 30+ trials) and found a
  // structural tension: simulationGravity's uniform center-pull and
  // simulationRepulsion's close-range separating kick both scale by the
  // same alpha, so damping the far field down also damped the overlap kick.
  // DRAG_REPULSION_BOOST (below) resolves that by scaling repulsion alone,
  // via a live config mutation independent of alpha — see its own comment.
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
  // Proportion-inversion diagnosis (round 4): cosmos renders a point's
  // diameter as a FIXED screen-px value, decoupled from the simulation's
  // space units. Verified against the unminified bundle's
  // calculatePointSize() shader: with scalePointsOnZoom=false (our config,
  // also cosmos's own default), `pSize = size * ratio * clamp(zoom*0.01, 1,
  // 5)` — for any zoom <=100 (true at every fitView this realm has produced)
  // the clamp pins to 1, so rendered diameter is MIN/MAX_POINT_SIZE
  // verbatim, in screen px, regardless of how compact or sprawling the
  // settled layout is in space units. That decoupling is why growing point
  // size alone (an earlier tuning pass) could never fix "nodes overlap,
  // edges invisible": the fix has to grow the SPACE-unit distance between
  // connected points, not the points themselves. cosmos's default
  // simulationLinkDistance — the spring's rest-length target, see
  // SETTLE_SIMULATION_LINK_DISTANCE below — is 10, tiny relative to the
  // settled spread the prior gravity/repulsion pair (1.3/0.4) produced, so a
  // connected pair's on-screen edge length was consistently shorter than the
  // fixed point diameters at both ends: edges rendered (pixel-sampled: edge
  // color, not background) but fully occluded by their own endpoints.
  //
  // Repulsion falloff (verified against the unminified bundle's Barnes-Hut
  // shader, ForceManyBody's calculateAdditionalVelocity): `addV = alpha *
  // repulsion * cellMass / dist` — repulsion decays as 1/distance, not a
  // constant push and not 1/distance^2. This is why a bounded equilibrium
  // (and therefore an orphan "ring" at a roughly fixed radius, rather than
  // unbounded drift) is possible at all: gravity's per-point pull GROWS
  // linearly with distance from center (forceGravity shader: `velocity +=
  // alpha * gravity * dist * 0.1`, unchanged from the prior tuning pass's
  // finding), while repulsion's push SHRINKS with distance — every point
  // settles where the two curves cross.
  //
  // Retuned (round 4, empirically, Playwright against this repo's own
  // 358-node/331-edge realm — texture/composition/ring/size/settle gate
  // definitions live in the tuning session's scratchpad, not in this file):
  // SETTLE_SIMULATION_LINK_DISTANCE (new — cosmos's rest length otherwise
  // defaults to 10) grows the spring's target so connected pairs settle far
  // enough apart, in space units, that their on-screen edge length clears
  // the fixed point diameter by a wide margin (median edge length / median
  // node diameter measured ~6.5-8x across repeated fresh mounts — Obsidian's
  // own reference screenshots show a comparable ratio). Gravity/repulsion
  // (0.65/2, both retuned) and repulsion theta (2, coarser than cosmos's
  // default 1.15 Barnes-Hut approximation — a small, consistent improvement
  // to the ring below with no measured cost to texture or settle time)
  // balance the larger rest length back down to a structure that fills a
  // majority of the fitted viewport (connected bounding box measured
  // ~55-65% of the viewport's smaller dimension across repeated fresh
  // mounts) rather than the prior pair's tiny, deeply-zoomed-out core.
  //
  // KNOWN LIMITATION (not further tunable within cosmos's uniform force
  // model — gravity/repulsion apply the same coefficients to every point
  // regardless of connectivity; only the spring, which only PULLS a
  // connected peripheral node IN toward its neighbor, differentiates
  // connected from orphan at all): repeated fresh-mount sampling puts the
  // orphan ring's median radius at ~0.9-1.2x the connected structure's own
  // max radius — straddling the "clearly a ring outside the structure"
  // threshold roughly a third of the time rather than reliably clearing it.
  // This realm has several small disconnected sub-clusters, not one single
  // hub; Barnes-Hut repulsion pushes a small sub-cluster outward from the
  // main mass the same way it pushes a lone orphan, with no extra gravity
  // restraint per member, so it can extend the connected set's own max
  // radius past the orphan ring's median on any given fresh mount. The ring
  // is still visually present and consistent from run to run (orphan-radius
  // coefficient of variation measured ~0.16-0.20, a tight band) — it just
  // doesn't clear the structure's outermost point by as wide a margin as
  // Obsidian's reference images show.
  var SETTLE_SIMULATION_DECAY = 430;
  var SETTLE_SIMULATION_GRAVITY = 0.65;
  var SETTLE_SIMULATION_REPULSION = 2;
  var SETTLE_SIMULATION_REPULSION_THETA = 2;

  // DRAG_REPULSION_BOOST scales simulationRepulsion (only) for the duration
  // of a drag, restored to SETTLE_SIMULATION_REPULSION in onDragEnd before
  // the release cooldown runs — so the overlap-separating kick gets
  // boosted energy while the user is actively dragging, without also
  // boosting simulationGravity's uniform far-field pull (a separate config
  // field, read independently — verified against the unminified 3.1.0
  // bundle's ForceManyBody/ForceGravity uniform builders) and without
  // perturbing the settle-tuned cooldown physics. graph.setConfigPartial()
  // is the live mechanism: it shallow-merges the given keys into the same
  // config object ForceManyBody.run() reads simulationRepulsion from every
  // tick (`repulsion: this.config.simulationRepulsion` inline in the
  // uniform builder, dist/index.js ~line 2218) — no position reset, no GPU
  // buffer reallocation, no branch in updateStateFromConfig() for this
  // field at all, so the change is live on the very next simulation step.
  //
  // Tuned empirically (Playwright against real /graph/data,
  // tmp/cosmos-tune/drag-repulsion-sweep.js, boost factors 1x-8x, 10 trials
  // each on a typical moderate-degree node pair): 1x (no boost) resolved
  // overlap on 8/10 and stayed calm on 8/10; 2x/3x/4x all cleared overlap
  // resolution and far-field calm (<=25% of drag displacement) on 9-10/10;
  // 6x/8x reliably resolved overlap but blew the far-field budget on every
  // trial (repulsion is a global per-tick force in cosmos's Barnes-Hut
  // pass, not local to the drop point, so a large enough boost measurably
  // moves points well outside the drag's neighborhood too). 3x is the
  // middle of the clean-pass band: full 10/10 overlap resolution (the more
  // central "repulsion resolves overlaps" behavior) with only one far-field
  // miss (a 29% overshoot, not wildly off) across 10 trials.
  var DRAG_REPULSION_BOOST = 3;

  // SETTLE_SIMULATION_LINK_DISTANCE is the spring's target rest length, in
  // space units (cosmos default: 10 — see the proportion-inversion diagnosis
  // above). SETTLE_SIMULATION_LINK_SPRING is the spring's stiffness
  // coefficient — verified against the unminified bundle's ForceLink shader:
  // the velocity contribution scales with `linkSpring * alpha *
  // (distance-to-restLength)/distance`, a unitless multiplier, not a
  // distance — kept at cosmos's default (1); no combination tried in this
  // pass's Playwright sweep improved on it enough to justify deviating.
  var SETTLE_SIMULATION_LINK_DISTANCE = 120;
  var SETTLE_SIMULATION_LINK_SPRING = 1;

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

  function updateGraphBtnState(active) {
    var btn = document.getElementById('btn-graph');
    if (!btn) { return; }
    btn.setAttribute('aria-pressed', active ? 'true' : 'false');
    if (active) { btn.classList.add('btn-graph-active'); }
    else { btn.classList.remove('btn-graph-active'); }
  }

  // ── Node type resolution (shared by styling and the profile's meta hooks) ──

  // typeOf resolves a point index's type (an opaque taxonomy key the profile
  // assigns via its adapt() — OKF type for the docs profile), defaulting to
  // 'page' — the same fallback atomicCyStyle() uses for untyped nodes.
  function typeOf(adapted, index) {
    var n = adapted.nodes[index];
    return (n && n.data && n.data.type) || 'page';
  }

  // ── Styling: degree-based sizing, profile-supplied palette + edge styling ──

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
  // grows the click/hover target, not just the pixels.
  //
  // GOTCHA (found this round, applyStyling's comment above has the full
  // trace): every prior value here (16-54, then 32-70) was configured but
  // never actually reached the GPU — Graph#create() doesn't re-derive
  // GraphData's per-point size field from what setPointSizes() staged, so
  // every real mount rendered cosmos's flat pointDefaultSize (4px)
  // regardless of this constant. Once applyStyling's trailing call was
  // fixed to render() (which does re-derive), these values render close to
  // 1:1 as screen-px diameter at the fitted view (verified: at zoom~3.3,
  // scalePointsOnZoom's off-branch clamp — min(5,max(1,zoom*0.01)) — is
  // pinned to 1, so calculatePointSize is a straight pass-through).
  //
  // Retuned down (round 4, alongside the link-distance fix above): the wider
  // SETTLE_SIMULATION_LINK_DISTANCE now gives edges plenty of screen length
  // on its own, so the point-size ceiling no longer has to do double duty
  // compensating for cramped layouts — MAX=24 keeps a degree-16 hub's
  // rendered circle from swallowing its neighbors even where two separate
  // degree-16-capped hubs land close together (this realm has more than
  // one, and the extra margin below 34 absorbs a pixel-measurement's own
  // uncertainty when two hub circles sit close enough to abut), and MIN=13
  // stays a comfortable click target while giving the 12px-floor gate a
  // pixel of headroom against antialiasing noise.
  var MIN_POINT_SIZE = 13, MAX_POINT_SIZE = 24;

  // sizeForDegree: linear map from degree range [0,DEGREE_CAP] to point-size
  // range [MIN_POINT_SIZE,MAX_POINT_SIZE].
  function sizeForDegree(deg) {
    return MIN_POINT_SIZE + (Math.min(deg, DEGREE_CAP) / DEGREE_CAP) * (MAX_POINT_SIZE - MIN_POINT_SIZE);
  }

  // computeLinkStyling assigns per-link color/width by delegating each link's
  // classes string to the profile's linkStyle() hook — cosmos.gl links carry
  // no dash-pattern API (checked against the unminified 3.1.0 .d.ts:
  // GraphConfigInterface has no line-style option), so the distinct-styling
  // contract for provenance/kind edges is COLOR (+ width) only, same as
  // before the extraction; only the per-classes DECISION (which colors/
  // widths a given edge-kind vocabulary maps to — "fingerprint"/"drift"/
  // "wikilink" for the docs profile, "contains" vs "calls"/"imports" for a
  // future code profile) moved to the profile. This function stays the
  // mechanical array-building loop.
  function computeLinkStyling(adapted, colors, profile) {
    var n = adapted.linkClasses.length;
    var linkColors = new Float32Array(n * 4);
    var linkWidths = new Float32Array(n);
    for (var i = 0; i < n; i++) {
      var styled = profile.linkStyle(adapted.linkClasses[i], colors);
      linkColors[i * 4] = styled.color[0];
      linkColors[i * 4 + 1] = styled.color[1];
      linkColors[i * 4 + 2] = styled.color[2];
      linkColors[i * 4 + 3] = styled.color[3];
      linkWidths[i] = styled.width;
    }
    return { colors: linkColors, widths: linkWidths };
  }

  // computeNodeColors builds the per-point RGBA array from each node's type,
  // resolved through the profile's colors() palette (atomicCyTypeColors() for
  // the docs profile — single source of truth, also used by the rail). A
  // filtered-out type gets alpha 0 rather than being dropped from the array:
  // the point stays in the sim (no reflow), just invisible — the hover/click
  // guard (in mount()) is what actually excludes it from interaction, since
  // alpha-0 points still GPU-pick in cosmos.
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
  // the CURRENT profile.colors() (re-read live so a theme flip picks up the
  // new CSS vars) and the current filteredTypes set. Called once at mount, on
  // every legend toggle, and from the theme-toggle's retheme() hook below.
  //
  // GOTCHA (found via Playwright pixel-measurement against the unminified
  // 3.1.0 bundle: getPointColors()/getPointSizes() both read back as flat
  // cosmos defaults — #b3b3b3 / 4px — on every real mount, not our per-node
  // arrays): setPointColors/setPointSizes/setLinkColors/setLinkWidths only
  // stage an `inputX` field and flip an `isXUpdateNeeded` flag
  // (dist/index.js Graph#setPointSizes etc). The derived field consumers
  // actually render from (GraphData#pointSizes, #pointColors, ...) is only
  // recomputed by GraphData#update() — and GraphData#update() is called
  // exclusively from Graph#render(), NOT from Graph#create() (Graph#create()
  // dispatches straight to the Points/Lines GPU-buffer builders, which read
  // the ALREADY-derived fields verbatim). Since mount()'s one-time initial
  // render() runs BEFORE this function's first call, every post-render call
  // here used to end on create() and silently push the stale pre-styling
  // snapshot (mount's default-color/default-size scatter) to the GPU
  // forever — sizes and OKF colors never took effect. render() (no args:
  // "keeps current alpha", per the .d.ts) re-runs GraphData#update() so the
  // just-set input arrays actually land, while still leaving simulation
  // state untouched — a legend toggle or theme flip still must never reflow
  // the layout, and render() satisfies that the same way create() did.
  function applyStyling(graph, adapted, filteredTypes, degrees, profile) {
    var colors = profile.colors();
    graph.setPointColors(computeNodeColors(adapted, colors, filteredTypes));
    graph.setPointSizes(computeNodeSizes(degrees));
    var linkStyle = computeLinkStyling(adapted, colors, profile);
    graph.setLinkColors(linkStyle.colors);
    graph.setLinkWidths(linkStyle.widths);
    graph.render();
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
  // Merges z/px/py into the EXISTING query string rather than replacing it
  // wholesale — code-graph checkpoint 6 adds `view`/`member` params this
  // function knows nothing about (the profile's own concern, not the
  // core's), and a wholesale rebuild would silently drop them on the very
  // next pan/zoom, breaking the "reload restores the same view" contract.
  function writeViewToURL(graph, container) {
    if (window.location.pathname !== '/graph') { return; }
    var center = graph.screenToSpacePosition([container.clientWidth / 2, container.clientHeight / 2]);
    var params = new URLSearchParams(window.location.search);
    params.set('z', graph.getZoomLevel().toFixed(4));
    params.set('px', center[0].toFixed(2));
    params.set('py', center[1].toFixed(2));
    try { window.history.replaceState(window.history.state, '', '/graph?' + params.toString()); } catch (e) {}
  }

  function clearLoading(mainPane) {
    var l = mainPane && mainPane.querySelector('.system-graph-loading');
    if (l) { l.remove(); }
  }

  // showError reuses the loading element as the error affordance — replacing
  // its text in place rather than removing it — mirroring the fetch .catch
  // pattern this replaces. The system-graph-error class (SC7's "not-indexed"
  // message polish) drops the loading state's italic transient styling so a
  // terminal message (e.g. "index not available — run atomic code index")
  // reads as a settled statement, not an in-progress spinner caption.
  function showError(mainPane, message) {
    var l = mainPane && mainPane.querySelector('.system-graph-loading');
    if (l) { l.textContent = message; l.classList.add('system-graph-error'); }
  }

  // teardown destroys the live instance (releasing its GL context — browsers
  // cap live contexts, so a leaked instance per open/close cycle eventually
  // blacks out the view) and drops system mode. Safe with no live instance:
  // a mid-fetch nav-out clears activeContainer below, so the pending .then
  // bails before ever constructing one.
  //
  // onTeardown() is optional on the profile contract — every other UI
  // touchpoint (onHover/onHoverOut/onClick) already routes through profile
  // hooks; a live-instance cleanup callback (dismissing whatever hover/click
  // UI the profile opened — AtomicGraphUI's preview card/page modal for the
  // docs profile) stays profile-supplied too, rather than hardcoding a
  // docs-shell global into this view-agnostic core.
  function teardown() {
    activeContainer = null;
    if (instance) {
      try { instance.destroy(); } catch (e) {}
      instance = null;
    }
    document.body.classList.remove('mode-system');
    updateGraphBtnState(false);
    if (activeProfile && activeProfile.onTeardown) { activeProfile.onTeardown(); }
    activeProfile = null;
  }

  // mount is called by a profile's own exported mount() (system-graph.js's
  // htmx.onLoad delegation, for instance) whenever that view's fragment
  // container lands in the DOM — `profile` supplies everything view-specific
  // (data fetch + cache key, response adapter, palette, meta/label
  // resolvers, hover/click hooks); see this file's header comment for the
  // full contract.
  function mount(container, profile) {
    if (container.dataset.systemMounted === '1') { return; } // double-mount guard
    // A prior mount's instance is torn down here, before this one begins —
    // the code-graph checkpoint 6 Docs|Code switcher calls mount() directly
    // on a FRESH container to swap profiles, with no intervening teardown()
    // call: teardown() also drops mode-system/btn-graph state, which the
    // switcher wants to KEEP across the swap (still in graph mode, just a
    // different graph). At most one cosmos.gl instance may ever be live —
    // browsers cap live GL contexts, same reasoning as teardown()'s own
    // comment. A plain graph-mode entry (nothing mounted yet) finds
    // `instance` already null here, so this is a no-op for that flow.
    if (instance) {
      try { instance.destroy(); } catch (e) {}
      instance = null;
      if (activeProfile && activeProfile.onTeardown) { activeProfile.onTeardown(); }
    }
    container.dataset.systemMounted = '1';
    activeContainer = container;
    activeProfile = profile;

    var mainPane = document.getElementById('main-pane');
    document.body.classList.add('mode-system');
    updateGraphBtnState(true);

    if (!isWebGL2Available()) {
      showError(mainPane, 'Your browser does not support WebGL2, which the Network View requires.');
      return;
    }

    var fingerprint = null;

    profile.fetchData()
      .then(function(fetched) {
        fingerprint = fetched.cacheKey;
        return loadCachedLayout(fingerprint).then(function(stored) { return { elems: fetched.elements, stored: stored }; });
      })
      .then(function(bundle) {
        if (activeContainer !== container) { return; } // torn down mid-fetch

        var adapted = profile.adapt(bundle.elems);
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
        // re-renders the card there via the profile's onHover hook —
        // spaceToScreenPosition is a pure camera-matrix multiply, not a GPU
        // readback, so calling it from the tick/zoom handlers below stays
        // clear of SC1's idle per-frame-work ban.
        function reanchorHoverCard() {
          if (hoverIndex == null || !hoverSpacePos) { return; }
          if (filteredTypes[typeOf(adapted, hoverIndex)]) { return; }
          var screenPos = graph.spaceToScreenPosition(hoverSpacePos);
          profile.onHover(profile.nodeMeta(adapted, hoverIndex), { x: screenPos[0], y: screenPos[1] }, container);
        }

        function ensureLabelLayer() {
          if (labelLayer) { return labelLayer; }
          labelLayer = document.createElement('div');
          labelLayer.className = 'graph-label-layer';
          container.appendChild(labelLayer);
          return labelLayer;
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
              el.textContent = profile.labelText(adapted, id);
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
            profile.onHoverOut();
          }
          applyStyling(graph, adapted, filteredTypes, degrees, profile);
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
        // cooldown reaches rest — not at raw pointer-release, since nothing
        // is pinned during the drag and the rest of the graph can still
        // nudge positions while cooling. Also called from onDragStart so a
        // second drag started before the first one's cooldown finishes
        // doesn't lose the write.
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
          simulationRepulsionTheta: SETTLE_SIMULATION_REPULSION_THETA,
          simulationLinkDistance: SETTLE_SIMULATION_LINK_DISTANCE,
          simulationLinkSpring: SETTLE_SIMULATION_LINK_SPRING,
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
            if (mountFinished) { return; } // a post-drag cooldown, not the initial mount — nothing else to do
            mountFinished = true;
            var saved = readViewFromURL();
            if (saved) {
              graph.setZoomTransformByPointPositions(new Float32Array([saved.pan.x, saved.pan.y]), 0, saved.zoom, 0, false);
            } else {
              graph.fitView(undefined, undefined, false);
            }
            buildLegend(adapted, profile.colors(), mainPane, onLegendToggle);
            clearLoading(mainPane);
            updateLabels();
            recording = true;
          },
          // Live reheat, no pinning. cosmos already glues the dragged point
          // to the pointer every frame regardless of pin state — its
          // dragPointCommand shader overwrites that point's position
          // unconditionally whenever dragInstance.isActive, verified against
          // the unminified 3.1.0 bundle (dist/index.js's Points.drag():
          // `if (index >= 0.0 && index == pointPosition.b) { pointPosition.rg
          // = mousePos.rg; }`, called from the render loop independent of
          // isSimulationRunning). start(alpha) here exists only to resume the
          // tick loop so springs pull direct neighbors and n-body repulsion
          // pushes apart whatever the drop overlaps — boosted for the
          // duration of the drag via DRAG_REPULSION_BOOST (see its comment).
          //
          // GOTCHA verified against the unminified 3.1.0 bundle: config.d.ts
          // types e.subject as {index, position} (its `Hovered` alias), but
          // the compiled runtime never overrides d3-drag's default subject
          // function — e.subject is actually {x, y} screen coordinates, so
          // e.subject.index is always undefined. graph.store.draggingPointIndex
          // is what cosmos itself sets from store.hoveredPoint.index
          // immediately before invoking this callback, and is the one live
          // source of the dragged point's index (empirically confirmed;
          // store isn't in the public .d.ts, but it's a plain instance
          // property, not a private field).
          onDragStart: function() {
            var idx = graph.store && graph.store.draggingPointIndex;
            if (idx == null) { return; }
            flushPendingSave();
            draggedIndex = idx;
            graph.setConfigPartial({ simulationRepulsion: SETTLE_SIMULATION_REPULSION * DRAG_REPULSION_BOOST });
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
            // Restored before the cooldown runs, not after — the release
            // settle (springs/repulsion still nudging positions while alpha
            // decays to the pause threshold) uses the same normal-strength
            // repulsion as a fresh mount, not the drag's boosted value.
            graph.setConfigPartial({ simulationRepulsion: SETTLE_SIMULATION_REPULSION });
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
            profile.onHoverOut();
            updateLabels();
          },
          onPointClick: function(index, pointPosition) {
            if (filteredTypes[typeOf(adapted, index)]) { return; }
            profile.onClick(adapted.indexToId[index], profile.nodeMeta(adapted, index));
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
        instance.__atomicRetheme = function() { applyStyling(graph, adapted, filteredTypes, degrees, profile); };
        // __atomicDebug (test-only, read-only) — exposes just enough of this
        // closure's already-computed state for debugState() below, which
        // scripts/graph-gates.mjs (SC3 gate harness) polls via page.evaluate.
        // Nothing here is mutated by debugState() — it only reads.
        instance.__atomicDebug = { adapted: adapted, degrees: degrees, cacheHit: hit };
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
        applyStyling(graph, adapted, filteredTypes, degrees, profile);
      })
      .catch(function(e) {
        if (activeContainer !== container) { return; }
        container.dataset.systemMounted = ''; // allow a retry on re-navigation
        // e.message surfaces a profile-thrown fetch error verbatim (the code
        // profile's fetchData() rejects with the server's own JSON error
        // body — codegraph.go's graphErrorResponse, e.g. "unknown member: x"
        // — docs/spec/code-graph.md checkpoint 5's "show the response's JSON
        // error in the pane"); the generic fallback covers errors with no
        // useful message (a thrown non-Error, or none at all).
        showError(mainPane, (e && e.message) || 'Could not render the system graph.');
        console.error('graph-core mount:', e);
      });
  }

  // retheme re-pushes point/link colors from the live cosmos.gl instance's
  // stored adapter/filter state — called by the shell's theme-toggle handler.
  // A no-op with no live instance (page view, or mid-fetch before the
  // instance is constructed).
  function retheme() {
    if (instance && instance.__atomicRetheme) { instance.__atomicRetheme(); }
  }

  // debugState (test-only, read-only) — recomputes every node's current
  // SPACE position (the simulation's own coordinates — camera-independent,
  // what the layout cache stores) and SCREEN position (for mouse-driven
  // gates: drag targeting, hover), plus rendered size (diameter,
  // sizeForDegree), from the live cosmos instance on every call (never
  // cached). Also reports isSimulationRunning and whether the current mount
  // replayed from the layout cache. Returns null with no live instance.
  // Consumed by scripts/graph-gates.mjs (SC3 gate harness) via
  // page.evaluate — never called from production code.
  //
  // space vs. screen matters for the cache-replay zero-motion gate
  // specifically: fitView()/setZoomTransformByPointPositions() (called once
  // per mount, right after settle) animate the CAMERA over a short
  // transition, so screen positions can still be moving for a beat after
  // mount even though the simulation itself is fully at rest — only space
  // coordinates isolate "did the cached layout replay without simulation
  // drift" from "is the camera still easing into its fitted view."
  //
  // O(n) per call (iterates every node) — fine at the harness's current
  // call-counts (point-in-time position samples only), but simRunning()
  // below is the cheap accessor for a per-tick POLLING loop (e.g. a
  // page.waitForFunction awaiting post-drag cooldown).
  function debugState() {
    if (!instance || !instance.__atomicDebug) { return null; }
    var dbg = instance.__atomicDebug;
    var flat = instance.getPointPositions();
    var nodes = {};
    for (var i = 0; i < dbg.adapted.nodes.length; i++) {
      var space = [flat[i * 2], flat[i * 2 + 1]];
      var screen = instance.spaceToScreenPosition(space);
      nodes[dbg.adapted.indexToId[i]] = {
        space: { x: space[0], y: space[1] },
        screen: { x: screen[0], y: screen[1] },
        size: sizeForDegree(dbg.degrees[i])
      };
    }
    return { isSimulationRunning: instance.isSimulationRunning, cacheHit: dbg.cacheHit, nodes: nodes };
  }

  // simRunning (test-only, read-only) — O(1) sibling of debugState() for
  // polling loops that only need to know whether the simulation is still
  // ticking, not per-node positions. Returns false with no live instance
  // (a poll waiting on "settled" reads that as already-settled, matching
  // debugState()'s own null-instance contract of reporting nothing to wait
  // on).
  function simRunning() {
    return !!(instance && instance.isSimulationRunning);
  }

  return {
    mount: mount, teardown: teardown, retheme: retheme,
    // Exported for scripts/test-system-graph-culling.cjs — the pure culling
    // policy behind SC5's label overlay, plus its tunable defaults.
    computeLabelSet: computeLabelSet,
    LABEL_CAP: LABEL_CAP,
    LABEL_FADE_ZOOM_THRESHOLD: LABEL_FADE_ZOOM_THRESHOLD,
    // Exported for scripts/graph-gates.mjs (SC3 gate harness) — see
    // debugState's/simRunning's own comments.
    debugState: debugState,
    simRunning: simRunning,
    // hexToRGBA01 is exposed for profiles that need to convert their own
    // palette hex values into cosmos's [r,g,b,a] 0..1 format (e.g. the docs
    // profile's fingerprint/drift provenance edge colors).
    hexToRGBA01: hexToRGBA01
  };
}());
