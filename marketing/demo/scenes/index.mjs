import docs from './docs.mjs';
import schema from './schema.mjs';
import chat from './chat.mjs';
import plans from './plans.mjs';
import graph from './graph.mjs';

// Tour order. A scene is { name, run(page, ctx), viewport?, tapes? }.
export const SCENES = [docs, schema, chat, plans, graph];
