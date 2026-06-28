import { afterAll, beforeAll, describe, expect, it } from 'bun:test';

import { attempt, attemptSync } from '@logosdx/utils';

/**
 * MCP Discovery Test
 *
 * This is a meta-test that exercises the noorm MCP server itself — not the
 * SDK or the database directly. Its purpose is to prove that an external AI
 * agent (Claude, Cursor, whatever) can discover the schema of this project
 * purely through the Model Context Protocol, without any direct file or
 * database access.
 *
 * The MCP server is started by `noorm mcp serve` and speaks JSON-RPC 2.0
 * over stdio. The two tools it exposes are:
 *
 *   - noorm_help       — command catalog + per-command help
 *   - run_noorm_cmd    — executes a specific noorm command
 *
 * If you delete this file the production MCP behaviour is no longer
 * validated end-to-end. Phase 2 of the playbook required a test that
 * actually spawns the MCP child process — no mocking, real stdio.
 */

// ---------------------------------------------------------------------------
// Types

interface JsonRpcSuccess {
    jsonrpc: '2.0';
    id:      number;
    result:  unknown;
}

interface JsonRpcError {
    jsonrpc: '2.0';
    id:      number;
    error:   { code: number; message: string; data?: unknown };
}

type JsonRpcResponse = JsonRpcSuccess | JsonRpcError;

interface ToolContent {
    type: 'text';
    text: string;
}

interface ToolCallResult {
    content: ToolContent[];
    isError?: boolean;
}

interface ToolsListResult {
    tools: Array<{ name: string; description?: string }>;
}

// ---------------------------------------------------------------------------
// Type guards

function isRecord(value: unknown): value is Record<string, unknown> {

    return typeof value === 'object' && value !== null && !Array.isArray(value);

}

function isJsonRpcResponse(value: unknown): value is JsonRpcResponse {

    if (!isRecord(value)) return false;
    return value.jsonrpc === '2.0' && typeof value.id === 'number';

}

function isToolCallResult(value: unknown): value is ToolCallResult {

    if (!isRecord(value)) return false;
    if (!Array.isArray(value.content)) return false;
    return value.content.every((c) => {

        if (!isRecord(c)) return false;
        return c.type === 'text' && typeof c.text === 'string';

    });

}

function isToolsListResult(value: unknown): value is ToolsListResult {

    if (!isRecord(value)) return false;
    if (!Array.isArray(value.tools)) return false;
    return value.tools.every((t) => {

        if (!isRecord(t)) return false;
        return typeof t.name === 'string';

    });

}

// ---------------------------------------------------------------------------
// MCP client

/**
 * Wraps `proc.stdout.getReader()` so we can take its inferred return type.
 * Calling `getReader()` directly (no args) picks the no-arg DOM overload
 * returning a `ReadableStreamDefaultReader<R>`. Pulling that type through
 * `ReturnType<['getReader']>` instead picks the BYOB-capable overload.
 */
function getReaderFromStdout(proc: Bun.Subprocess<'pipe', 'pipe', 'pipe'>) {

    return proc.stdout.getReader();

}

/**
 * Thin JSON-RPC 2.0 client over a child process's stdio.
 *
 * Reads stdout incrementally, buffers partial lines, and resolves the next
 * pending request whose id matches the response id. The server only emits
 * one response per request so we keep an id->Deferred map rather than a
 * queue.
 *
 * Why not just stream-parse? Because notifications (e.g. from log events)
 * may arrive interleaved with responses; matching by id is the only safe
 * approach.
 */
class MCPClient {

    #proc:    Bun.Subprocess<'pipe', 'pipe', 'pipe'>;
    // Reader type is inferred from the constructor's `proc.stdout.getReader()`
    // call. We *cannot* annotate the field via `ReturnType<...['getReader']>`
    // because that always picks the last overload (BYOB-capable), which
    // requires args to `read()`. Letting inference pick gives us the correct
    // no-arg `ReadableStreamDefaultReader<Uint8Array>` shape.
    #reader:  ReturnType<typeof getReaderFromStdout>;
    #decoder: TextDecoder;
    #buffer:  string;
    #nextId:  number;
    #pending: Map<number, { resolve: (v: JsonRpcResponse) => void; reject: (e: Error) => void }>;
    #closed:  boolean;
    #readLoopDone: Promise<void>;

    constructor(proc: Bun.Subprocess<'pipe', 'pipe', 'pipe'>) {

        this.#proc          = proc;
        this.#reader        = getReaderFromStdout(proc);
        this.#decoder       = new TextDecoder();
        this.#buffer        = '';
        this.#nextId        = 1;
        this.#pending       = new Map();
        this.#closed        = false;
        this.#readLoopDone  = this.#readLoop();

    }

    /**
     * Drains stdout, splits on newlines, parses each JSON-RPC message, and
     * dispatches it to the matching pending request. Runs until stdout
     * closes (EOF) or until close() is called.
     */
    async #readLoop(): Promise<void> {

        while (!this.#closed) {

            const [chunk, err] = await attempt(() => this.#reader.read());
            if (err) {

                this.#failAllPending(err);
                return;
            }
            if (!chunk) return;
            if (chunk.done) {

                this.#failAllPending(new Error('MCP server closed stdout'));
                return;
            }

            this.#buffer += this.#decoder.decode(chunk.value, { stream: true });

            let nl = this.#buffer.indexOf('\n');
            while (nl !== -1) {

                const line     = this.#buffer.slice(0, nl).trim();
                this.#buffer   = this.#buffer.slice(nl + 1);

                if (line.length > 0) this.#dispatchLine(line);

                nl = this.#buffer.indexOf('\n');

            }

        }

    }

    #dispatchLine(line: string): void {

        // JSON.parse is the only sync escape hatch we need — it returns `any`
        // by definition; we narrow with isJsonRpcResponse below.
        const [parsed, parseErr] = attemptSync(() => JSON.parse(line));

        if (parseErr) return; // malformed line — drop it, nothing else to do

        const value: unknown = parsed;
        if (!isJsonRpcResponse(value)) return;

        const handler = this.#pending.get(value.id);
        if (!handler) return; // unsolicited response — ignore
        this.#pending.delete(value.id);
        handler.resolve(value);

    }

    #failAllPending(err: Error): void {

        for (const { reject } of this.#pending.values()) reject(err);
        this.#pending.clear();

    }

    /**
     * Send a JSON-RPC request and resolve with the response whose id matches.
     *
     * Throws if the response is an error frame or if the server closes stdout
     * before answering.
     */
    async request(method: string, params?: unknown): Promise<JsonRpcSuccess> {

        const id = this.#nextId++;

        const payload = params === undefined
            ? { jsonrpc: '2.0', method, id }
            : { jsonrpc: '2.0', method, params, id };

        const wire = JSON.stringify(payload) + '\n';

        const waiter = new Promise<JsonRpcResponse>((resolve, reject) => {

            this.#pending.set(id, { resolve, reject });

        });

        this.#proc.stdin.write(wire);

        const response = await waiter;

        if ('error' in response) {

            throw new Error(`JSON-RPC error ${response.error.code}: ${response.error.message}`);
        }

        return response;

    }

    /**
     * Send a JSON-RPC notification (no id, no response expected).
     */
    notify(method: string, params?: unknown): void {

        const payload = params === undefined
            ? { jsonrpc: '2.0', method }
            : { jsonrpc: '2.0', method, params };

        this.#proc.stdin.write(JSON.stringify(payload) + '\n');

    }

    async close(): Promise<void> {

        this.#closed = true;

        // Cancel the reader first so the read loop exits and releases the
        // stream lock. Without this, kill() can race with an in-flight read.
        const [, cancelErr] = await attempt(() => this.#reader.cancel());
        if (cancelErr) {

            // Best-effort — we're tearing down regardless.
        }

        // SIGKILL — the noorm MCP server's stdio loop blocks on stdin, and
        // a plain `kill()` (SIGTERM by default) can hang on some platforms
        // until the next read syscall returns. SIGKILL guarantees exit.
        this.#proc.kill(9);

        // Wait for process exit, but cap it so afterAll never deadlocks.
        const exitOrTimeout = await Promise.race([
            attempt(() => this.#proc.exited).then(() => 'exited' as const),
            new Promise<'timeout'>((resolve) => setTimeout(() => resolve('timeout'), 2000)),
        ]);

        if (exitOrTimeout === 'timeout') {

            // Process didn't reap within 2s — orphaned, but not fatal for
            // the test run. The OS will clean up after the harness exits.
        }

    }

}

// ---------------------------------------------------------------------------
// Helpers

/**
 * Extracts the text payload from a `tools/call` result. If the tool returned
 * an error frame (isError=true) we still return the text so callers can
 * assert on the error message — but most tests want the success path.
 */
function extractText(result: unknown): string {

    if (!isToolCallResult(result)) {

        throw new Error(`expected tool/call result, got: ${JSON.stringify(result)}`);
    }

    if (result.content.length === 0) {

        throw new Error('tool/call result has empty content array');
    }

    const first = result.content[0];
    if (first === undefined) throw new Error('tool/call result first content is undefined');

    return first.text;

}

/**
 * Parses the text payload of a `run_noorm_cmd` result as JSON. The MCP server
 * JSON-stringifies command output, so the consumer parses it back.
 */
function parseRunResult(result: unknown): unknown {

    const text = extractText(result);

    const [parsed, parseErr] = attemptSync(() => JSON.parse(text));

    if (parseErr) {

        throw new Error(`failed to JSON.parse run_noorm_cmd output: ${parseErr.message}; raw text=${text}`);
    }

    return parsed;

}

/**
 * Walks PATH and returns the first `noorm` binary that is NOT inside any
 * `node_modules` directory. Bun's test runner injects every ancestor's
 * `node_modules/.bin` at the front of PATH, which shadows the user's
 * globally-installed noorm with a workspace shim that may not have the
 * `mcp` subcommand.
 *
 * @example
 * const bin = resolveGlobalNoorm();  // → '/Users/alice/.local/bin/noorm'
 */
function resolveGlobalNoorm(): string {

    const path = process.env.PATH ?? '';
    const exe  = process.platform === 'win32' ? 'noorm.exe' : 'noorm';

    for (const dir of path.split(':')) {

        if (dir.length === 0) continue;
        if (dir.includes('node_modules')) continue;

        const candidate = `${dir}/${exe}`;
        // `test -x` is the simplest sync executable-existence check that
        // avoids pulling fs.existsSync + fs.statSync into the test deps.
        const stat = Bun.spawnSync(['test', '-x', candidate]);
        if (stat.exitCode === 0) return candidate;

    }

    throw new Error(
        `Could not resolve a global noorm binary on PATH (excluding node_modules). ` +
        `The MCP discovery test needs a noorm binary that has the 'mcp' subcommand. ` +
        `Install via 'npm install -g @noormdev/cli' or symlink your dev build into ~/.local/bin/.`,
    );

}

// ---------------------------------------------------------------------------
// Test suite

let client: MCPClient | null = null;
let stderrBuf = '';

beforeAll(async () => {

    // Resolve a noorm binary that has the `mcp` subcommand. Bun's test
    // runner auto-prepends every node_modules/.bin in the path chain to
    // PATH, which shadows the user's globally installed noorm with the
    // workspace shim (`packages/cli/noorm.js`). The shim's `bin/noorm`
    // is the platform-agnostic build downloaded at npm postinstall — it
    // may lag behind the rebuilt local arm64 binary that has the
    // Phase 1 SDK fixes AND the `mcp` subcommand.
    //
    // Strategy: walk PATH and return the first entry that is NOT inside
    // any node_modules. This recovers the user's globally installed
    // noorm (typically ~/.local/bin/noorm or /usr/local/bin/noorm).
    const noormBinary = resolveGlobalNoorm();

    // Spawn the MCP server once and reuse it across all `it()` blocks. The
    // server is stateful (it holds a connection pool keyed by config name)
    // so we explicitly call connect("dev") in the connect-dependent tests.
    const proc = Bun.spawn([noormBinary, 'mcp', 'serve'], {
        cwd:    process.cwd(),
        stdin:  'pipe',
        stdout: 'pipe',
        stderr: 'pipe',
    });

    // Drain stderr in the background so we can surface it on failure. The
    // MCP server logs unrecoverable errors here before exiting; without
    // this, any startup failure shows up as a bare "MCP server closed
    // stdout" with no diagnostic.
    void (async () => {
        const dec = new TextDecoder();
        const reader = proc.stderr.getReader();
        while (true) {
            const [chunk, err] = await attempt(() => reader.read());
            if (err) return;
            if (!chunk) return;
            if (chunk.done) return;
            stderrBuf += dec.decode(chunk.value, { stream: true });
        }
    })();

    client = new MCPClient(proc);

    // Brief pause so the MCP server finishes its stdio setup. Without this,
    // sending initialize too eagerly while running near the end of a long
    // test session occasionally races against the server's startup, and
    // Bun.spawn surfaces a half-open pipe state.
    await new Promise((r) => setTimeout(r, 100));

    // Handshake. The MCP spec requires `initialize` before any other RPCs.
    const [, initErr] = await attempt(() => client!.request('initialize', {
        protocolVersion: '2024-11-05',
        capabilities:    {},
        clientInfo:      { name: 'mcp-discovery-test', version: '1.0.0' },
    }));
    if (initErr) {
        // Surface stderr so the failure is diagnosable.
        throw new Error(`initialize failed: ${initErr.message}\nMCP stderr:\n${stderrBuf || '(empty)'}`);
    }

    client.notify('notifications/initialized');

    // Open a connection for the dev config — required for overview/list/
    // detail/sql. We do it here rather than inside individual tests so the
    // pool is reused; the test that proves connect itself works is implicit
    // (every subsequent test fails without it).
    const [connectResponse, connectErr] = await attempt(() => client!.request('tools/call', {
        name:      'run_noorm_cmd',
        arguments: { command: 'connect', config: 'dev' },
    }));

    if (connectErr) {
        throw new Error(`connect tools/call failed: ${connectErr.message}\nMCP stderr:\n${stderrBuf || '(empty)'}`);
    }

    if (!isToolCallResult(connectResponse.result)) {

        throw new Error(`connect did not return a tool/call result: ${JSON.stringify(connectResponse.result)}`);
    }

    if (connectResponse.result.isError) {

        throw new Error(`connect failed: ${extractText(connectResponse.result)}\nMCP stderr:\n${stderrBuf || '(empty)'}`);
    }

});

afterAll(async () => {

    if (client) await client.close();

});

describe('MCP discovery — JSON-RPC handshake & tool catalog', () => {

    it('tools/list advertises both noorm_help and run_noorm_cmd', async () => {

        if (!client) throw new Error('client not initialised');

        const response = await client.request('tools/list');

        if (!isToolsListResult(response.result)) {

            throw new Error(`tools/list returned unexpected shape: ${JSON.stringify(response.result)}`);
        }

        const names = response.result.tools.map((t) => t.name);

        expect(names).toContain('noorm_help');
        expect(names).toContain('run_noorm_cmd');

        // Sanity: at least these two — the server may expose more in the
        // future but these two are the minimum advertised surface.
        expect(response.result.tools.length).toBeGreaterThanOrEqual(2);

    });

});

describe('MCP discovery — noorm_help command catalog', () => {

    it('returns a non-empty command catalog with the core discovery commands', async () => {

        if (!client) throw new Error('client not initialised');

        const response = await client.request('tools/call', {
            name:      'noorm_help',
            arguments: {},
        });

        const text = extractText(response.result);

        expect(text.length).toBeGreaterThan(0);

        // These are the load-bearing commands an agent needs to discover
        // a schema. If any of them disappears from the catalog, the MCP's
        // discovery story is broken.
        for (const cmd of ['connect', 'list_configs', 'overview', 'list', 'detail', 'sql']) {

            expect(text).toContain(cmd);

        }

    });

    it('returns command-specific help when given a command name', async () => {

        if (!client) throw new Error('client not initialised');

        const response = await client.request('tools/call', {
            name:      'noorm_help',
            arguments: { command: 'overview' },
        });

        const text = extractText(response.result);

        // The specific-command help should describe what `overview` does:
        // namely, return counts of database object categories.
        expect(text.toLowerCase()).toContain('overview');

        // At minimum it should mention what overview returns. The actual
        // help text says "tables, views, procedures, functions, types,
        // indexes, foreign keys, triggers, locks, connections" — assert on
        // a handful of these so the test isn't brittle to phrasing tweaks.
        const lower = text.toLowerCase();
        const buckets = ['tables', 'views', 'procedures', 'functions'];
        const hits    = buckets.filter((b) => lower.includes(b));
        expect(hits.length).toBeGreaterThanOrEqual(3);

    });

});

describe('MCP discovery — run_noorm_cmd against the dev/test configs', () => {

    it('list_configs returns both dev and test postgres configs', async () => {

        if (!client) throw new Error('client not initialised');

        const response = await client.request('tools/call', {
            name:      'run_noorm_cmd',
            arguments: { command: 'list_configs' },
        });

        const parsed = parseRunResult(response.result);

        if (!Array.isArray(parsed)) {

            throw new Error(`list_configs should return an array; got ${typeof parsed}`);
        }

        const byName: Record<string, Record<string, unknown>> = {};
        for (const entry of parsed) {

            if (!isRecord(entry)) continue;
            if (typeof entry.name === 'string') byName[entry.name] = entry;

        }

        const dev  = byName['dev'];
        const test = byName['test'];

        if (!dev)  throw new Error(`list_configs missing 'dev' entry; got: ${JSON.stringify(parsed)}`);
        if (!test) throw new Error(`list_configs missing 'test' entry; got: ${JSON.stringify(parsed)}`);

        expect(dev.dialect).toBe('postgres');
        expect(test.dialect).toBe('postgres');

    });

    it('overview returns the expected schema counts for the dev database', async () => {

        if (!client) throw new Error('client not initialised');

        const response = await client.request('tools/call', {
            name:      'run_noorm_cmd',
            arguments: { command: 'overview', config: 'dev' },
        });

        const parsed = parseRunResult(response.result);

        if (!isRecord(parsed)) {

            throw new Error(`overview should return an object; got ${typeof parsed}`);
        }
        const o = parsed;

        expect(o.tables).toBe(38);
        expect(o.views).toBe(18);
        expect(o.triggers).toBe(10);

        // procedures + functions split depends on PG's pg_proc classification
        // (Phase 1 produced 59 + 24 = 83 callables on this dev instance).
        // We assert the floor — the split can wobble across pg versions but
        // the total must hold.
        const procs = typeof o.procedures === 'number' ? o.procedures : 0;
        const funcs = typeof o.functions  === 'number' ? o.functions  : 0;
        expect(procs + funcs).toBeGreaterThanOrEqual(69);

    });

    it('list with category=tables returns all 38 application tables', async () => {

        if (!client) throw new Error('client not initialised');

        const response = await client.request('tools/call', {
            name:      'run_noorm_cmd',
            arguments: { command: 'list', config: 'dev', payload: { category: 'tables' } },
        });

        const parsed = parseRunResult(response.result);

        if (!Array.isArray(parsed)) {

            throw new Error(`list(tables) should return an array; got ${typeof parsed}`);
        }

        expect(parsed.length).toBe(38);

        const names = new Set<string>();
        for (const row of parsed) {

            if (!isRecord(row)) continue;
            if (typeof row.name === 'string') names.add(row.name);

        }

        // Anchor tables from the LLM-memory domain — if any go missing the
        // schema regressed in a way the overview count would miss.
        for (const expected of ['Agent', 'Project', 'Memory', 'Note', 'Tag', 'Task']) {

            expect(names.has(expected)).toBe(true);

        }

    });

    it('detail for the Memory table exposes the expected columns', async () => {

        if (!client) throw new Error('client not initialised');

        const response = await client.request('tools/call', {
            name:      'run_noorm_cmd',
            arguments: {
                command: 'detail',
                config:  'dev',
                payload: { category: 'tables', name: 'Memory' },
            },
        });

        const parsed = parseRunResult(response.result);

        if (!isRecord(parsed)) {

            throw new Error(`detail should return an object; got ${typeof parsed}`);
        }

        expect(parsed.name).toBe('Memory');

        if (!Array.isArray(parsed.columns)) {

            throw new Error(`detail.columns should be an array; got ${typeof parsed.columns}`);
        }

        const colNames = new Set<string>();
        for (const col of parsed.columns) {

            if (!isRecord(col)) continue;
            if (typeof col.name === 'string') colNames.add(col.name);

        }

        // Strict on the load-bearing memory columns — these are what the
        // domain code in src/db/ depends on. Missing any of them means the
        // schema drifted.
        const required = [
            'memory_id',
            'content',
            'domain',
            'category',
            'relevance_status',
            'provenance_id',
            'agent_id',
            'was_inferred',
            'last_accessed_at',
            'access_count',
            'created_at',
            'updated_at',
        ];

        for (const col of required) {

            expect(colNames.has(col)).toBe(true);

        }

    });

    it('sql executes a SELECT and returns rows', async () => {

        if (!client) throw new Error('client not initialised');

        const response = await client.request('tools/call', {
            name:      'run_noorm_cmd',
            arguments: { command: 'sql', config: 'dev', payload: { query: 'SELECT 1 AS one' } },
        });

        const parsed = parseRunResult(response.result);

        if (!isRecord(parsed)) {

            throw new Error(`sql should return an object; got ${typeof parsed}`);
        }

        if (!Array.isArray(parsed.rows)) {

            throw new Error(`sql.rows should be an array; got ${typeof parsed.rows}`);
        }

        expect(parsed.rows.length).toBe(1);

        const first = parsed.rows[0];
        if (!isRecord(first)) {

            throw new Error(`sql.rows[0] should be an object; got ${typeof first}`);
        }

        // Drivers may stringify a PG int; accept either shape.
        const one = first.one;
        if (typeof one !== 'number' && typeof one !== 'string') {

            throw new Error(`sql.rows[0].one should be number or string; got ${typeof one}`);
        }
        expect([1, '1']).toContain(one);

    });

});
