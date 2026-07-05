#!/usr/bin/env node
'use strict';

// Unit test for system-graph.js's exported label-culling policy (SC5,
// cosmos-system-graph checkpoint 5). NOT a CI gate — this repo has no
// headless-browser step, so there is nowhere in CI to run it. Run manually
// after touching the label-overlay code in
// atomic/internal/serve/assets/system-graph.js:
//
//   node scripts/test-system-graph-culling.cjs
//
// Loads the REAL system-graph.js source in a minimal window/document
// sandbox — the file's only top-level (load-time) DOM calls are two
// document.addEventListener() registrations, stubbed here as no-ops and
// never invoked — then calls window.SystemGraph.computeLabelSet() directly
// with synthetic viewport/degree/zoom fixtures. No cosmos.gl instance, no
// browser, no mocked internals: this exercises the shipped function.

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const SRC_PATH = path.join(
  __dirname, '..', 'atomic', 'internal', 'serve', 'assets', 'system-graph.js'
);

function loadSystemGraph() {
  const source = fs.readFileSync(SRC_PATH, 'utf8');

  // Run in THIS realm (not vm.createContext's separate one) so every array
  // computeLabelSet returns is an ordinary main-realm Array — directly
  // comparable via assert.deepEqual against literals below. A fresh
  // vm.createContext() realm would make even a same-shape `[]` fail
  // deepStrictEqual's cross-realm reference/prototype check.
  const previousWindow = global.window;
  const previousDocument = global.document;
  global.window = {};
  global.document = { addEventListener: function() {} };
  vm.runInThisContext(source, { filename: SRC_PATH });
  const sg = global.window.SystemGraph;
  global.window = previousWindow;
  global.document = previousDocument;

  assert.ok(sg && typeof sg.computeLabelSet === 'function',
    'system-graph.js must export window.SystemGraph.computeLabelSet');
  return sg;
}

// Cap: more on-screen candidates than the cap must yield exactly the cap,
// keeping the highest-degree candidates.
function testCapEnforcement(sg) {
  const total = 200;
  const candidates = [];
  for (let i = 0; i < total; i++) {
    candidates.push({ id: 'n' + i, x: 100, y: 100, degree: i });
  }
  const viewport = { width: 800, height: 600 };
  const result = sg.computeLabelSet(candidates, viewport, sg.LABEL_FADE_ZOOM_THRESHOLD + 1, {});

  assert.equal(result.render.length, 150,
    'cap must bound render to exactly 150 when 200 candidates are on-screen');

  const renderedDegrees = result.render.map((id) => Number(id.slice(1))).sort((a, b) => b - a);
  const expectedTop150 = Array.from({ length: 150 }, (_, i) => total - 1 - i); // 199..50
  assert.deepEqual(renderedDegrees, expectedTop150,
    'cap must keep the 150 highest-degree candidates, not an arbitrary 150');
}

// Zoom threshold boundary: strictly below fades every rendered label; at or
// above the threshold, none are faded.
function testZoomThresholdBoundary(sg) {
  const candidates = [
    { id: 'a', x: 10, y: 10, degree: 5 },
    { id: 'b', x: 20, y: 20, degree: 3 }
  ];
  const viewport = { width: 800, height: 600 };
  const threshold = 1.5;

  const below = sg.computeLabelSet(candidates, viewport, threshold - 0.001, { fadeZoomThreshold: threshold });
  assert.deepEqual(below.faded.slice().sort(), ['a', 'b'],
    'below the zoom threshold every rendered label must be faded');

  const atThreshold = sg.computeLabelSet(candidates, viewport, threshold, { fadeZoomThreshold: threshold });
  assert.deepEqual(atThreshold.faded, [],
    'at (or above) the zoom threshold no label should be faded');
}

// Viewport exclusion: off-screen candidates are excluded from render
// regardless of degree.
function testViewportExclusion(sg) {
  const viewport = { width: 800, height: 600 };
  const candidates = [
    { id: 'onscreen', x: 400, y: 300, degree: 1 },
    { id: 'left-of-viewport', x: -10, y: 300, degree: 99 },
    { id: 'below-viewport', x: 400, y: 601, degree: 99 }
  ];
  const result = sg.computeLabelSet(candidates, viewport, sg.LABEL_FADE_ZOOM_THRESHOLD + 1, {});
  assert.deepEqual(result.render, ['onscreen'],
    'off-screen candidates must be excluded even at a much higher degree');
}

// Hover exception: the hovered id always renders and is never faded, even
// off-screen, at zero degree, below the fade threshold, and over the cap.
function testHoverException(sg) {
  const viewport = { width: 800, height: 600 };
  const crowd = [];
  for (let i = 0; i < 200; i++) { crowd.push({ id: 'crowd' + i, x: 1, y: 1, degree: 1000 }); }
  const hovered = { id: 'hovered-offscreen', x: -500, y: -500, degree: 0 };
  const candidates = crowd.concat([hovered]);

  const result = sg.computeLabelSet(candidates, viewport, 0, { hoveredId: hovered.id });

  assert.ok(result.render.includes(hovered.id),
    'the hovered id must render regardless of viewport, zoom, degree, or cap');
  assert.ok(!result.faded.includes(hovered.id),
    'the hovered id must never be faded');
  assert.equal(result.render.length, 151,
    'render must be the cap (150) plus the forced hovered id');
}

function main() {
  const sg = loadSystemGraph();
  const tests = [
    ['cap enforcement (highest-degree wins)', testCapEnforcement],
    ['zoom threshold boundary', testZoomThresholdBoundary],
    ['viewport exclusion', testViewportExclusion],
    ['hover exception', testHoverException]
  ];

  let failures = 0;
  tests.forEach(([name, fn]) => {
    try {
      fn(sg);
      console.log('PASS  ' + name);
    } catch (err) {
      failures++;
      console.error('FAIL  ' + name);
      console.error('      ' + err.message);
    }
  });

  if (failures > 0) {
    console.error('\n' + failures + '/' + tests.length + ' test(s) failed.');
    process.exitCode = 1;
  } else {
    console.log('\nAll ' + tests.length + ' test(s) passed.');
  }
}

main();
