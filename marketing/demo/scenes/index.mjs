import docs from './docs.mjs';
import search from './search.mjs';
import schema from './schema.mjs';
import chat from './chat.mjs';
import plans from './plans.mjs';
import external from './external.mjs';
import graph from './graph.mjs';
import about from './about.mjs';

// Tour order. A scene is { name, run(page, ctx), viewport?, tapes?, room?, speed?, warm? }.
// Adding one: create scenes/<name>.mjs, import it here, place it, then
// `node marketing/demo/run.mjs` records just that one and re-stitches.
export const SCENES = [docs, search, schema, chat, plans, external, graph, about];
