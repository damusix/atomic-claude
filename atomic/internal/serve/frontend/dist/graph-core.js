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

  // computeAdjacency builds a point index -> {neighbors, links} lookup ONCE
  // per data load (called alongside computeDegrees in mount()) — the hover
  // highlight handlers below (onPointMouseOver/onLinkMouseOver) read this
  // instead of walking adapted.links per event, so a hover on the 17.5k-node/
  // 54k-edge code graph costs one array lookup, not an O(E) scan. neighbors
  // and links are parallel per point index: adjacency.links[i][k] is the
  // link index connecting point i to adjacency.neighbors[i][k].
  function computeAdjacency(adapted) {
    var n = adapted.nodes.length;
    var neighbors = new Array(n);
    var links = new Array(n);
    for (var i = 0; i < n; i++) { neighbors[i] = []; links[i] = []; }
    var pairs = adapted.links;
    var linkCount = pairs.length / 2;
    for (var li = 0; li < linkCount; li++) {
      var a = pairs[li * 2], b = pairs[li * 2 + 1];
      neighbors[a].push(b);
      neighbors[b].push(a);
      links[a].push(li);
      links[b].push(li);
    }
    return { neighbors: neighbors, links: links };
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
  //
  // Retuned down again (graph-interactions brief, 2026-07-08): 8-14px
  // requested directly, not re-derived from a fresh Playwright pixel sweep —
  // the render/hit-test mechanics established above (1:1 screen-px diameter
  // at the fitted view, GPU picking against that same rendered size) are
  // unaffected by the number, only the target range moved down. Neither
  // zoom bound below is numerically tied to this pair: ZOOM_MAX=500 is the
  // vendored shader's own fixed saturation point, independent of any
  // app-level size constant (see its own comment), and the zoom-out floor
  // is fit-anchored, not size-derived (see effectiveZoomMin's comment — a
  // node-size-derived floor was tried and found empirically wrong). Hit-
  // testing samples the rendered size (findHoveredPoint, same comment
  // above), so whether an 8px MIN floor stays reliably hoverable/draggable
  // is gate-verified (pixel sweep pending), not derived — this comment
  // records the target, not a proof.
  var MIN_POINT_SIZE = 8, MAX_POINT_SIZE = 14;

  // ── Zoom-scaled point sizes (2026-07-18 user feedback) ────────────────────
  //
  // In screen-space-constant mode (scalePointsOnZoom=false) a point's
  // rendered diameter never shrinks as the camera zooms out (see ZOOM_MAX's
  // comment), so a fitted view of a dense graph reads as a solid mass of
  // full-size dots. cosmos's pointSizeScale config is a live uniform
  // multiplied into calculatePointSize() AND both GPU pick passes
  // (findHoveredPoint/findPointsInRect read the same sizeScale uniform —
  // verified against the vendored bundle), so scaling it per zoom shrinks
  // dots and their hit targets together, O(1) per zoom event, no per-point
  // array rebuild. Policy: full size at/above POINT_SCALE_FULL_ZOOM_X times
  // the mount's own fit zoom, shrinking linearly with zoom-out, floored so
  // the smallest dot never drops below POINT_SCALE_MIN_PX. Anchored to the
  // dataset's fit zoom (same reasoning as effectiveZoomMin — natural fit
  // zooms differ wildly between the docs realm and the 17.5k-node code
  // graph, so a fixed zoom threshold can't work for both).
  var POINT_SCALE_MIN_PX = 4;
  var POINT_SCALE_FULL_ZOOM_X = 2.5;

  // ── Zoom clamp (item 2/3, graph-interactions brief) ────────────────────────
  //
  // cosmos has no scaleExtent/min-max-zoom config field (checked against the
  // unminified 3.1.0 .d.ts: GraphConfigInterface carries initialZoomLevel/
  // enableZoom/onZoom* only — the underlying d3-zoom behavior's own
  // scaleExtent is hardcoded to [.001, Infinity] inside the bundle and not
  // exposed), so the clamp below is enforced from the onZoom handler via
  // setZoomLevel — see mount()'s onZoom for the userDriven guard that keeps
  // this from fighting its own corrective call.
  //
  // ZOOM_MAX is derived from calculatePointSize() (verified against the
  // unminified bundle, dist/index.js ~4394: with scalePointsOnZoom=false —
  // this config's setting — `pSize = size * ratio * clamp(k*0.01, 1, 5)`,
  // then `return min(pSize, maxPointSize * ratio)`), where k is the raw
  // zoom level (getZoomLevel()/e.transform.k). `maxPointSize` in that final
  // clamp is a STORE field, not this app's MAX_POINT_SIZE constant — it's
  // `getMaxPointSize(device, pixelRatio)` (dist/index.js ~260), which reads
  // the WebGL context's `ALIASED_POINT_SIZE_RANGE[1]` hardware limit (64px
  // fallback when unavailable) divided by pixelRatio, entirely independent
  // of anything this app configures. Screen-space-constant mode: the
  // `clamp(k*0.01,1,5)` term's max(1.0,...) never drops below 1, so a
  // point's apparent CSS-px size is PINNED at its own `size` value for
  // every k in (0, 100] — it never shrinks as the camera zooms out, only
  // grows once k exceeds 100, capped at 5x `size` beyond k=500 (subject to
  // the separate hardware-derived `maxPointSize` ceiling above, whichever
  // is smaller).
  //
  // ZOOM_MAX: k=500 is calculatePointSize's own growth-saturation point for
  // the `clamp(k*0.01,1,5)` term — beyond it, a point's apparent diameter
  // from THIS term gets literally zero larger (the separate hardware
  // maxPointSize ceiling may cap it even earlier on constrained GPUs), so
  // further zoom-in is pure downside (world spreads out with no
  // point-legibility gain from this term) with no upside. This is the
  // closest node-size-derived analog to "zooming in to absurdity" this API
  // surface offers — verified correct independent of the maxPointSize
  // correction above, since that ceiling only ever makes k=500 MORE
  // conservative (an earlier real-world cap), never less. Empirically
  // confirmed working by the orchestrator's browser gate — unlike the
  // zoom-out floor below, this one needed no correction.
  var ZOOM_MAX = 500;

  // The zoom-out floor is NOT a fixed, node-size-derived constant. An
  // earlier version of this file computed one (MAX_POINT_SIZE divided by a
  // "typical settled edge length" derived from SETTLE_SIMULATION_LINK_DISTANCE)
  // on the same reasoning as ZOOM_MAX above — but the orchestrator's browser
  // gate caught it empirically wrong: point SIZE never shrinks with
  // zoom-out in this engine's screen-space-constant mode (see ZOOM_MAX's
  // comment), so a floor derived from node size alone sat roughly an order
  // of magnitude below any real fitted view and never actually engaged —
  // wheel-out collapsed the docs graph to a ~28px speck before the old
  // floor caught it. The DATASET's own world-space footprint is what
  // varies (docs realm vs. the 17.5k-node code graph land at wildly
  // different natural fit zooms), so the floor has to be anchored to THAT,
  // not to a fixed point-size ratio. effectiveZoomMin below is that
  // fit-anchored floor, computed once per mount from the actual settled
  // layout (computeFitZoomApprox) rather than derived in advance from a
  // constant.

  // POINT_GREYOUT_OPACITY (item 5) — cosmos already darkens/lightens a
  // greyed-out point's COLOR for free once highlightedPointIndices is set
  // (verified against the unminified bundle's point fragment shader: the
  // isDarkenGreyout branch fires with no config needed), but leaves alpha
  // untouched unless this is set (its own default is `undefined`, which the
  // shader reads as "no additional alpha multiply" — greyoutOpacity!=-1
  // gate). Set explicitly so "everything else dims" is an unambiguous
  // opacity drop, not just a color-recolor a user could miss at a glance.
  // linkGreyoutOpacity needs no matching override — cosmos's own default
  // (0.1) already dims links.
  var POINT_GREYOUT_OPACITY = 0.15;

  // sizeForDegree: linear map from degree range [0,DEGREE_CAP] to point-size
  // range [MIN_POINT_SIZE,MAX_POINT_SIZE].
  function sizeForDegree(deg) {
    return MIN_POINT_SIZE + (Math.min(deg, DEGREE_CAP) / DEGREE_CAP) * (MAX_POINT_SIZE - MIN_POINT_SIZE);
  }

  // quintileForDegree: linear map from degree range [0,DEGREE_CAP] to ramp
  // shade [1,5] — the SAME scale sizeForDegree uses, so a node's shade and
  // its size move together (docs/spec/code-graph.md: "shade reinforces
  // [size], it doesn't diverge from it") rather than a percentile/rank-based
  // quintile that could put two similarly-sized nodes in different shades.
  function quintileForDegree(deg) {
    var q = Math.ceil((Math.min(deg, DEGREE_CAP) / DEGREE_CAP) * 5);
    return Math.max(1, Math.min(5, q));
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
  // the docs profile, code-graph.js's own colors() for the code profile —
  // also used by the rail/legend). A node's fill is its type's ramp shade at
  // shadeCurve[quintileForDegree(deg) - 1] (colors[type + '-ramp'][shade - 1]
  // — docs/spec/code-graph.md's degree-quintile shading; shade 2, the
  // legend-chip color, is the fallback when no ramp is present on the
  // palette). shadeCurve lets a profile remap which ramp shade each quintile
  // lands on — e.g. code-graph.js's dense leaf-heavy degree distribution
  // would otherwise put ~90% of nodes on shade 1 (every hue's pastel), so it
  // supplies a curve that floors leaves at shade 2. A filtered-out type gets
  // alpha 0 rather than being dropped from the array: the point stays in the
  // sim (no reflow), just invisible — the hover/click guard (in mount()) is
  // what actually excludes it from interaction, since alpha-0 points still
  // GPU-pick in cosmos.
  function computeNodeColors(adapted, colors, filteredTypes, degrees, shadeCurve) {
    var n = adapted.nodes.length;
    var out = new Float32Array(n * 4);
    for (var i = 0; i < n; i++) {
      var type = typeOf(adapted, i);
      var ramp = colors[type + '-ramp'] || colors['default-ramp'];
      var shade = shadeCurve[quintileForDegree(degrees[i]) - 1];
      var base = (ramp && ramp[shade - 1]) || colors[type] || colors['default-fill'];
      var rgba = hexToRGBA01(base, filteredTypes[type] ? 0 : 1);
      out[i * 4] = rgba[0];
      out[i * 4 + 1] = rgba[1];
      out[i * 4 + 2] = rgba[2];
      out[i * 4 + 3] = rgba[3];
    }
    return out;
  }

  // IDENTITY_SHADE_CURVE is the default shadeCurve — quintile N lands on
  // shade N verbatim. Used when a profile doesn't define shadeCurve (the
  // docs profile today), so computeNodeColors always has a curve to index.
  var IDENTITY_SHADE_CURVE = [1, 2, 3, 4, 5];

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
    // Optional profile-supplied quintile→shade remap (see computeNodeColors'
    // own comment) — validated to exactly 5 entries so a malformed profile
    // value can't index the ramp out of bounds; falls back to the identity
    // curve (docs profile's effective mapping is unchanged by this).
    var shadeCurve = (profile.shadeCurve && profile.shadeCurve.length === 5) ? profile.shadeCurve : IDENTITY_SHADE_CURVE;
    graph.setPointColors(computeNodeColors(adapted, colors, filteredTypes, degrees, shadeCurve));
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

  // clearLoading HIDES the loading element instead of removing it — the
  // element is rendered (and owned) by React's Graph route; removing a
  // React-owned node breaks React's own reconciliation on the next
  // Docs|Code switch ("Failed to execute 'insertBefore' on 'Node'": React
  // inserts the fresh keyed mount container relative to this <p> as its
  // recorded sibling anchor, which no longer exists). resetLoading is the
  // mount-time counterpart that re-shows it for the next mount.
  function clearLoading(mainPane) {
    var l = mainPane && mainPane.querySelector('.system-graph-loading');
    if (l) { l.style.display = 'none'; }
  }

  function resetLoading(mainPane) {
    var l = mainPane && mainPane.querySelector('.system-graph-loading');
    if (l) {
      l.style.display = '';
      l.classList.remove('system-graph-error');
      l.textContent = 'Laying out graph…';
    }
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
      if (instance.__atomicCleanup) { instance.__atomicCleanup(); }
      try { instance.destroy(); } catch (e) {}
      instance = null;
    }
    var hint = document.getElementById('graph-hint');
    if (hint) { hint.remove(); }
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
      if (instance.__atomicCleanup) { instance.__atomicCleanup(); }
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
    resetLoading(mainPane);

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
        // effectiveZoomMin: 0 is a placeholder for the pre-settle window
        // only — with no floor yet computed, `k < 0` never trips (zoom
        // levels are always positive), so onZoom's userDriven clamp below
        // is a safe no-op until onSimulationEnd sets the real fit-anchored
        // value (see that handler's own comment).
        var effectiveZoomMin = 0;
        var fitZoom = 0;             // computeFitZoomApprox() at settle — anchors the point-scale ramp below
        var currentPointScale = 1;
        // updatePointScale maps the current zoom onto pointSizeScale — see
        // POINT_SCALE_MIN_PX's comment for the policy and why it's fit-
        // anchored. 0.01 epsilon skips no-op config writes at wheel rates.
        function updatePointScale(k) {
          if (!fitZoom) { return; } // pre-settle: no anchor yet
          var minScale = POINT_SCALE_MIN_PX / MIN_POINT_SIZE;
          var s = Math.max(minScale, Math.min(1, k / (fitZoom * POINT_SCALE_FULL_ZOOM_X)));
          if (Math.abs(s - currentPointScale) < 0.01) { return; }
          currentPointScale = s;
          graph.setConfigPartial({ pointSizeScale: s });
          graph.render();
        }
        var degrees = computeDegrees(adapted);
        var adjacency = computeAdjacency(adapted); // built once per data load — see its own comment
        var filteredTypes = {};      // type -> true while its legend chip is toggled off (alpha-0 + hover/click guard)
        var hoverIndex = null;       // point index the preview card is currently anchored to, or null
        var hoverSpacePos = null;    // that point's last-known SPACE position, refreshed by onPointMouseOver/onSimulationTick
        var lastMouseClientPos = null; // {x,y} viewport coords, refreshed by onMouseMove — edge-hover's only anchor source (onLinkMouseOver gets no position argument, see its own comment)
        var labelCandidateIndices = rankByDegree(degrees); // top-N by degree, static for the session
        var labelPool = {};    // node id -> mounted <div class="graph-label"> element
        var labelLayer = null; // lazily-created overlay holding every label div, appended to `container`

        // setHighlight/clearHighlight drive cosmos's native highlightedPointIndices/
        // highlightedLinkIndices — NOT cheap, despite reading as "two array
        // lookups feed a config write": verified against the unminified 3.1.0
        // bundle's Points#updatePointStatus/Lines#updateLinkStatus (called
        // synchronously from setConfigPartial's updateStateFromConfig), each
        // call allocates a FULL Float32Array(textureSize^2 * 4) status buffer
        // from scratch and re-uploads it to the GPU as an rgba32float texture
        // — O(N)/O(M) in the point/link count, not in the highlighted subset
        // size. At this repo's own 17.5k-node/54k-edge code-view scale that's
        // ~283KB (point status) + ~868KB (link status) rebuilt and re-uploaded
        // on every call — doubled on a node->link hover transition (onPointMouseOut's
        // clearHighlight, then onLinkMouseOver's setHighlight, back to back).
        // Acceptable at human hover rates (an edge-triggered transition every
        // tens-to-hundreds of ms at most, never per animation frame — SC1's
        // idle-cost ban is about per-frame work, which this isn't), but NOT
        // free — the same-index guard in onPointMouseOver below exists because
        // of this real cost, not as a micro-optimization for its own sake. The
        // trailing render() mirrors applyStyling's own trailing call (same
        // GOTCHA class: a config change lands in the store but a repaint isn't
        // guaranteed without it).
        function setHighlight(pointIndices, linkIndices) {
          graph.setConfigPartial({ highlightedPointIndices: pointIndices, highlightedLinkIndices: linkIndices });
          graph.render();
        }
        function clearHighlight() {
          graph.setConfigPartial({ highlightedPointIndices: undefined, highlightedLinkIndices: undefined });
          graph.render();
        }

        // Pinned highlight (2026-07-18 user feedback): a Shift-click pins a
        // node's neighborhood emphasis so it survives mouse-out and Shift
        // release; a second Shift-click on the SAME node opens it (modal),
        // and a plain (no-Shift) click always opens directly. Unpinned by a
        // background click, by pinning another node, or by legend-hiding the
        // pinned node's type. applyHighlightState is the single resolver:
        // pin wins, then live Shift-hover, else clear — every handler that
        // used to call clearHighlight on its way out calls this instead so
        // a transient hover/edge highlight falls back to the pin, not to
        // nothing.
        var pinnedIndex = null;
        function applyHighlightState() {
          if (pinnedIndex != null) {
            setHighlight(adjacency.neighbors[pinnedIndex].concat([pinnedIndex]), adjacency.links[pinnedIndex]);
          } else if (shiftDown && hoverIndex != null && !filteredTypes[typeOf(adapted, hoverIndex)]) {
            setHighlight(adjacency.neighbors[hoverIndex].concat([hoverIndex]), adjacency.links[hoverIndex]);
          } else {
            clearHighlight();
          }
        }

        // linkHoverMeta builds the "A -kind-> B" preview-card meta for edge hover
        // (item 6) — reusing profile.labelText(), the same text the node hover card
        // and DOM labels already show for each endpoint, so no new lookup machinery.
        function linkHoverMeta(linkIndex, s, t) {
          var kind = adapted.linkClasses[linkIndex] || 'link';
          var sourceLabel = profile.labelText(adapted, adapted.indexToId[s]);
          var targetLabel = profile.labelText(adapted, adapted.indexToId[t]);
          return {
            type: 'edge',
            title: sourceLabel + ' —' + kind + '→ ' + targetLabel,
            description: kind,
            snippet: ''
          };
        }

        // linkHoverScreenPos anchors the edge-hover preview card at the pointer —
        // onLinkMouseOver(linkIndex) carries no position argument (checked against
        // the unminified 3.1.0 .d.ts: unlike onPointMouseOver, it's index-only), so
        // onMouseMove below is the only available position source. getBoundingClientRect
        // is deferred to here (link-hover-enter, edge-triggered) rather than run on
        // every mousemove, keeping the frequent side of this pair cheap.
        function linkHoverScreenPos() {
          if (!lastMouseClientPos) { return { x: 0, y: 0 }; }
          var rect = container.getBoundingClientRect();
          return { x: lastMouseClientPos.x - rect.left, y: lastMouseClientPos.y - rect.top };
        }

        // reanchorHoverCard re-projects hoverSpacePos into screen coordinates and
        // re-renders the card there via the profile's onHover hook —
        // spaceToScreenPosition is a pure camera-matrix multiply, not a GPU
        // readback, so calling it from the tick/zoom handlers below stays
        // clear of SC1's idle per-frame-work ban.
        function reanchorHoverCard() {
          if (!shiftDown) { return; } // hover card is Shift-gated — see setShift
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
          if (hidden && pinnedIndex != null && typeOf(adapted, pinnedIndex) === type) {
            // Hiding the pinned node's type unpins it — a highlight anchored
            // to an invisible point would be unreachable to dismiss.
            pinnedIndex = null;
            applyHighlightState();
          }
          if (hidden && hoverIndex != null && typeOf(adapted, hoverIndex) === type) {
            // F-5 carry-along: hiding the hovered node's own type would
            // otherwise leave the preview card frozen at its last position —
            // the point is now alpha-0 and guarded from hover/click, so its
            // card is unreachable until the type is shown again. Clear it.
            hoverIndex = null;
            hoverSpacePos = null;
            profile.onHoverOut();
            applyHighlightState();
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

        // computeFitZoomApprox (item 2/3 fix) derives a synchronous proxy for
        // "the zoom level that fits every current point in the viewport",
        // from the already-settled positions this exact event has ready
        // (same one-time getPointPositions() cost class as saveFullSnapshot
        // above — never per frame). NOT byte-identical to cosmos's own
        // fitView math: that reads its private getFitViewPositions()/
        // zoomInstance internals, and fitView's actual camera move is always
        // rAF-deferred (verified against the unminified 3.1.0 bundle:
        // setZoomTransformByPointPositions — what fitView delegates to —
        // always routes through `.transition()`, even at duration 0, so its
        // resulting zoom can't be read back synchronously right after
        // calling it). This is a plain bounding-box-to-viewport ratio
        // instead: k = min(viewportW/bboxW, viewportH/bboxH) is exactly the
        // scale factor that maps the bbox's space-width/height onto the
        // viewport's screen-px width/height, the same physical quantity
        // getZoomLevel() reports — accurate enough for the fit-anchored
        // zoom-out floor effectiveZoomMin derives from it (see
        // onSimulationEnd's own comment on why that floor moved off a
        // fixed node-size-derived constant).
        function computeFitZoomApprox() {
          var flat = graph.getPointPositions();
          // Degenerate no-points guard — unreachable in practice (mount()
          // never fires onSimulationEnd with zero nodes), and the return
          // value is moot either way (nothing to fit or zoom).
          if (!flat.length) { return 1; }
          var minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
          for (var i = 0; i < flat.length; i += 2) {
            var x = flat[i], y = flat[i + 1];
            if (x < minX) { minX = x; }
            if (x > maxX) { maxX = x; }
            if (y < minY) { minY = y; }
            if (y > maxY) { maxY = y; }
          }
          var bboxW = Math.max(1, maxX - minX), bboxH = Math.max(1, maxY - minY);
          var viewportW = container.clientWidth || 1, viewportH = container.clientHeight || 1;
          return Math.min(viewportW / bboxW, viewportH / bboxH);
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

        // Shift-gated interactivity (2026-07-18 user feedback): plain mouse
        // movement over a dense graph used to fade everything but the hovered
        // node's neighborhood and start node drags — making relationships
        // impossible to study and panning impossible when zoomed into a dense
        // region (every press landed on a point and dragged it instead of
        // panning). Hover highlight/dim, the preview card, and node drag now
        // engage only while Shift is held; without Shift the pointer is
        // purely a camera tool (pan/zoom) plus click-to-open. Labels still
        // track hover either way (non-disruptive). Listeners are document-
        // level, registered per mount, removed via __atomicCleanup in
        // teardown()/profile-swap.
        var shiftDown = false;
        function onShiftChange(e) {
          if (e.key !== 'Shift') { return; }
          setShift(e.type === 'keydown');
        }
        function onWindowBlur() { setShift(false); } // release stuck Shift on tab-away
        function setShift(down) {
          if (down === shiftDown) { return; }
          shiftDown = down;
          graph.setConfigPartial({ enableDrag: down });
          if (down && hoverIndex != null && !filteredTypes[typeOf(adapted, hoverIndex)]) {
            // Shift pressed while already hovering a node: light it up now.
            reanchorHoverCard();
          } else if (!down) {
            profile.onHoverOut();
          }
          applyHighlightState(); // pin survives Shift release — see its comment
        }
        document.addEventListener('keydown', onShiftChange);
        document.addEventListener('keyup', onShiftChange);
        window.addEventListener('blur', onWindowBlur);

        var graph = new Cosmos.Graph(container, {
          // fitViewOnInit would fit to the scatter/seed below, before the
          // simulation (or the cache replay) has settled — fit/restore
          // manually once onSimulationEnd fires.
          fitViewOnInit: false,
          // Drag is Shift-gated — see setShift above. Starts off so the
          // first interaction is always pan/zoom, never an accidental drag.
          enableDrag: false,
          simulationDecay: SETTLE_SIMULATION_DECAY,
          simulationGravity: SETTLE_SIMULATION_GRAVITY,
          simulationRepulsion: SETTLE_SIMULATION_REPULSION,
          simulationRepulsionTheta: SETTLE_SIMULATION_REPULSION_THETA,
          simulationLinkDistance: SETTLE_SIMULATION_LINK_DISTANCE,
          simulationLinkSpring: SETTLE_SIMULATION_LINK_SPRING,
          // pointGreyoutOpacity: see POINT_GREYOUT_OPACITY's own comment.
          // linkGreyoutOpacity is left at cosmos's own default (0.1).
          pointGreyoutOpacity: POINT_GREYOUT_OPACITY,
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
            // effectiveZoomMin (item 2/3 fix, corrected post-browser-gate):
            // fit-anchored, not node-size-derived — a fixed-constant floor
            // (MAX_POINT_SIZE / typical edge length) was tried first and
            // found empirically wrong: it sat ~10x below any real fitted
            // zoom for this repo's docs realm, so it never actually
            // engaged, and wheel-out collapsed the graph to a ~28px speck
            // before the old floor caught it. computeFitZoomApprox() reads
            // THIS dataset's own just-settled positions, so the floor
            // scales with whatever graph is actually mounted (docs realm
            // vs. the 17.5k-node code graph land at very different natural
            // fit zooms). *0.6 gives room to pull back a bit past the
            // fitted frame without letting the graph shrink toward a dot.
            // Computed unconditionally, before branching on `saved` below,
            // so a garbage/pre-clamp-era saved.zoom can never corrupt it.
            fitZoom = computeFitZoomApprox();
            effectiveZoomMin = fitZoom * 0.6;
            var saved = readViewFromURL();
            if (saved) {
              // Clamp a restored URL's zoom param into range — a pre-clamp-era
              // bookmark, or one saved during the exact race this fix closes,
              // must not resurrect an out-of-range view.
              var clampedSavedZoom = Math.max(effectiveZoomMin, Math.min(ZOOM_MAX, saved.zoom));
              graph.setZoomTransformByPointPositions(new Float32Array([saved.pan.x, saved.pan.y]), 0, clampedSavedZoom, 0, false);
            } else {
              // fitView's own programmatic move is never clamped — the floor
              // only bounds where the USER can zoom OUT to afterward (onZoom
              // below); fit must always be free to frame the graph.
              graph.fitView(undefined, undefined, false);
            }
            // Legend + hint mount into `container` (the keyed graph-mount
            // div, a React leaf with no reconciled children), NOT #main-pane —
            // React owns #main-pane's child list, and vanilla-inserted
            // siblings there corrupt its insertBefore anchors on a Docs|Code
            // switch. The keyed container is destroyed wholesale on switch,
            // so these clean themselves up with no removal bookkeeping.
            buildLegend(adapted, profile.colors(), container, onLegendToggle);
            if (!container.querySelector('.graph-hint')) {
              var hint = document.createElement('div');
              hint.id = 'graph-hint';
              hint.className = 'graph-hint';
              hint.textContent = 'hold ⇧ to highlight & drag · ⇧-click pins, again opens';
              container.appendChild(hint);
            }
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
            // Zoom clamp (item 2/3): only on userDriven zoom — cosmos reports
            // e.sourceEvent-less programmatic transforms (our own setZoomLevel
            // correction below, and mount's fitView/setZoomTransformByPointPositions)
            // as userDriven=false, so this can't re-trigger itself, and fitView's
            // own moves are never clamped (see onSimulationEnd's comment).
            // duration=0, enableSimulation=false: an immediate snap-back, no
            // wake of a paused sim. setZoomLevel(..., 0, ...) calls d3-zoom's
            // own scaleTo on the SAME behavior instance that handles
            // wheel/pinch input (verified against the unminified 3.1.0
            // bundle), so the next input event computes its delta from the
            // corrected value, not the pre-clamp one — no fighting/runaway
            // drift. effectiveZoomMin is fit-anchored per-mount state (see
            // onSimulationEnd's own comment for the derivation and why a
            // fixed node-size-derived floor was tried and rejected); it
            // starts at 0 (see its declaration) until that handler sets the
            // real value, so `k < effectiveZoomMin` is always false — a
            // no-op, not a wrongly-tight clamp — for any userDriven zoom
            // that somehow fires in the brief pre-settle window.
            if (userDriven) {
              var k = e.transform.k;
              if (k < effectiveZoomMin) { graph.setZoomLevel(effectiveZoomMin, 0, false); }
              else if (k > ZOOM_MAX) { graph.setZoomLevel(ZOOM_MAX, 0, false); }
            }
            // Applied for programmatic moves too (fitView's own transition
            // ends at the fitted zoom, which should land at the scaled-down
            // size) — userDriven is irrelevant to the size policy.
            updatePointScale(e.transform.k);
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
            // onPointMouseOver can refire for the SAME index while the
            // pointer sits still over one node (per the unminified 3.1.0
            // .d.ts: it's re-triggered by zoom/pan and by simulation-driven
            // point movement, not only by the pointer actually entering a
            // new point) — sameNode skips the setHighlight rebuild in that
            // case, since it's not cheap (see setHighlight's own comment).
            var sameNode = hoverIndex === index;
            hoverIndex = index;
            hoverSpacePos = pointPosition;
            updateLabels();
            // Without Shift, hover is passive: the label still tracks (via
            // updateLabels' hovered-id path) but no card, no highlight/dim —
            // see setShift's comment.
            if (!shiftDown) { return; }
            reanchorHoverCard();
            if (sameNode) { return; }
            // Item 5: emphasize this node + its neighbors + the edges between
            // them, dim everything else — adjacency was built once at mount,
            // so this is two array lookups, not a graph walk. A pinned node
            // keeps its highlight — hover doesn't override the pin.
            if (pinnedIndex == null) {
              setHighlight(adjacency.neighbors[index].concat([index]), adjacency.links[index]);
            }
          },
          onPointMouseOut: function() {
            hoverIndex = null;
            hoverSpacePos = null;
            profile.onHoverOut();
            updateLabels();
            applyHighlightState(); // falls back to the pin, or clears
          },
          onPointClick: function(index, pointPosition) {
            if (filteredTypes[typeOf(adapted, index)]) { return; }
            // Shift-click: first click pins this node's highlight; a second
            // Shift-click on the SAME node opens it. Plain click always opens.
            if (shiftDown && pinnedIndex !== index) {
              pinnedIndex = index;
              applyHighlightState();
              return;
            }
            profile.onClick(adapted.indexToId[index], profile.nodeMeta(adapted, index));
          },
          onBackgroundClick: function() {
            if (pinnedIndex == null) { return; }
            pinnedIndex = null;
            applyHighlightState();
          },
          // Item 6 (edge hover). Registering onLinkMouseOver/onLinkMouseOut is
          // what ENABLES cosmos's link hit-testing at all — verified against
          // the unminified 3.1.0 bundle: isLinkHoveringEnabled is false, and
          // store.hoveredLinkIndex never populates, unless at least one of
          // onLinkClick/onLinkContextMenu/onLinkMouseOver/onLinkMouseOut is
          // configured. Once enabled, the hovered link's own width bump
          // (hoveredLinkWidthIncrease, default 5px) is fully native — no
          // per-link restyling needed here, only the highlight-set + the
          // preview card (STEP 0: no cheap general line hit-testing existed
          // before this, so this native path is the only reason item 6 is
          // implementable at 54k-edge scale at all).
          onLinkMouseOver: function(linkIndex) {
            if (!shiftDown) { return; } // edge hover is Shift-gated too — see setShift
            var s = adapted.links[linkIndex * 2], t = adapted.links[linkIndex * 2 + 1];
            if (filteredTypes[typeOf(adapted, s)] || filteredTypes[typeOf(adapted, t)]) { return; }
            setHighlight([s, t], [linkIndex]);
            profile.onHover(linkHoverMeta(linkIndex, s, t), linkHoverScreenPos(), container);
          },
          onLinkMouseOut: function() {
            profile.onHoverOut();
            applyHighlightState(); // falls back to the pin, or clears
          },
          // onMouseMove only tracks the raw pointer position for
          // linkHoverScreenPos's anchor (see its own comment) — no other work,
          // so this stays cheap at mousemove frequency.
          onMouseMove: function(index, pointPosition, event) {
            lastMouseClientPos = { x: event.clientX, y: event.clientY };
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
        // Removes this mount's document/window listeners (Shift gating) —
        // fired by teardown() and by mount()'s prior-instance destroy path.
        instance.__atomicCleanup = function() {
          document.removeEventListener('keydown', onShiftChange);
          document.removeEventListener('keyup', onShiftChange);
          window.removeEventListener('blur', onWindowBlur);
        };
        // __atomicDebug (test-only, read-only) — exposes just enough of this
        // closure's already-computed state for debugState() below, which
        // scripts/graph-gates.mjs (SC3 gate harness) polls via page.evaluate.
        // Nothing here is mutated by debugState() — it only reads.
        instance.__atomicDebug = {
          adapted: adapted, degrees: degrees, cacheHit: hit,
          // Live getters (test-only): the SC3 harness and ad-hoc probes read
          // interaction state that lives in this closure.
          get pinnedIndex() { return pinnedIndex; },
          get hoverIndex() { return hoverIndex; },
          get shiftDown() { return shiftDown; }
        };
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
    return {
      isSimulationRunning: instance.isSimulationRunning, cacheHit: dbg.cacheHit, nodes: nodes,
      pinnedIndex: dbg.pinnedIndex, hoverIndex: dbg.hoverIndex, shiftDown: dbg.shiftDown
    };
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
