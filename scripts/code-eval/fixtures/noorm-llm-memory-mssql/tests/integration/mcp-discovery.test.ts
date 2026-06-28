/**
 * Layer 4 — MCP discovery integration test.
 *
 * Exercises the noorm MCP server (`noorm mcp serve`) end-to-end. Spawns
 * the CLI as a subprocess, connects via the MCP TypeScript SDK over
 * stdio, and asserts the two exposed tools (`noorm_help`, `run_noorm_cmd`)
 * behave as documented:
 *
 *   1. `noorm_help` (no args) lists at least the core commands.
 *   2. `run_noorm_cmd("overview")` returns the dev DB's expected counts.
 *   3. `run_noorm_cmd("list", { category: "types" })` returns the four
 *      composite TVP types defined in `sql/00_types/`.
 *   4. A bad command name yields `isError: true` and a follow-up valid
 *      call still succeeds — the server didn't crash.
 *
 * The MCP SDK is hoisted at the monorepo root (`@modelcontextprotocol/sdk`,
 * a noorm dependency). Bun resolves it via workspace hoisting; no example
 * package.json change is needed. If resolution ever breaks, switch to a
 * skipped suite with a gap row in `mssql-problems.md`.
 *
 * Spawned subprocess CWD must be the example dir so the MCP server
 * discovers `.noorm/settings.yml` and the active `dev` config. The test
 * issues an explicit `connect` first — `overview` and `list` need an
 * active session.
 */
import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js';

const PROJECT_ROOT = new URL('../..', import.meta.url).pathname;

let client: Client;
let transport: StdioClientTransport;

/**
 * Pull the first text payload out of an MCP tool call response.
 *
 * Every tool in noorm's MCP server (`src/mcp/server.ts`) returns
 * `content: [{ type: 'text', text: '...' }]`. The SDK's `callTool` return
 * type is a union of CallToolResult and a legacy compatibility envelope,
 * so we accept `unknown` and validate at runtime — same defensive shape
 * the SDK itself recommends in its samples.
 */
function firstText(result: unknown): string {

    if (typeof result !== 'object' || result === null) {

        throw new Error(`MCP tool call did not return an object; got: ${typeof result}`);
    }

    if (!('content' in result) || !Array.isArray(result.content)) {

        throw new Error(`MCP tool call did not return content array; got keys: ${Object.keys(result).join(', ')}`);
    }

    const block = result.content[0];

    if (typeof block !== 'object' || block === null) {

        throw new Error(`First content block is not an object; got: ${JSON.stringify(block)}`);
    }

    if (!('type' in block) || block.type !== 'text') {

        throw new Error(`First content block is not text; got type: ${'type' in block ? String(block.type) : '<missing>'}`);
    }

    if (!('text' in block) || typeof block.text !== 'string') {

        throw new Error(`First content block missing text string; got: ${JSON.stringify(block)}`);
    }

    return block.text;

}

/**
 * Read the `isError` flag from an MCP tool call response without widening
 * the SDK's union type. Returns `undefined` when the field is absent
 * (success envelopes from this server omit it).
 */
function isError(result: unknown): boolean | undefined {

    if (typeof result !== 'object' || result === null) return undefined;
    if (!('isError' in result)) return undefined;

    const value = result.isError;
    if (typeof value !== 'boolean') return undefined;

    return value;

}

beforeAll(async () => {

    // Build a string-only env. process.env declares values as `string | undefined`
    // but StdioClientTransport requires `Record<string, string>`. Drop the
    // undefined entries instead of casting.
    const env: Record<string, string> = {};
    for (const [key, value] of Object.entries(process.env)) {

        if (typeof value === 'string') env[key] = value;

    }

    transport = new StdioClientTransport({
        command: 'noorm',
        args: ['mcp', 'serve'],
        cwd: PROJECT_ROOT,
        // Inherit the parent env so PATH and the noorm identity are visible.
        env,
        // Pipe stderr so the MCP server's "running on stdio" banner doesn't
        // pollute test output. We don't read it, but piping prevents the
        // child from inheriting our terminal.
        stderr: 'pipe',
    });

    client = new Client({ name: 'mcp-discovery-test', version: '0.0.1' });
    await client.connect(transport);

    // The MCP server starts disconnected. `overview` and `list` both call
    // `session.getContext()`, which throws if no connection exists. Connect
    // once for the whole suite — the SessionManager memoises by config.
    // Connect with the `test` config — the one the suite bootstraps
    // (createContext({ config: 'test' }) in the helpers, and `noorm ci init
    // --name test` in CI). The `dev` config isn't provisioned under test.
    const connectRes = await client.callTool({
        name: 'run_noorm_cmd',
        arguments: { command: 'connect', config: 'test' },
    });

    expect(isError(connectRes)).toBeFalsy();

}, 30_000);

afterAll(async () => {

    // Disconnect the DB session before tearing down the transport so the
    // MCP child cleanly closes its Kysely pool. Best-effort — if the server
    // already exited we don't want to mask a real test failure.
    if (client) {

        await client.callTool({
            name: 'run_noorm_cmd',
            arguments: { command: 'disconnect' },
        }).catch(() => undefined);

        await client.close().catch(() => undefined);
    }

    if (transport) {

        await transport.close().catch(() => undefined);
    }

});

describe('mcp-discovery: noorm_help', () => {

    it('lists at least the core commands when called without arguments', async () => {

        const result = await client.callTool({ name: 'noorm_help', arguments: {} });
        const text = firstText(result);

        const requiredCommands = ['connect', 'overview', 'list', 'detail', 'sql'];

        for (const name of requiredCommands) {

            expect(text).toContain(`**${name}**`);

        }

    });

    it('returns detailed help for a specific command', async () => {

        const result = await client.callTool({
            name: 'noorm_help',
            arguments: { command: 'overview' },
        });

        expect(isError(result)).toBeFalsy();

        const text = firstText(result);

        // The detailed help text always names the command somewhere; we don't
        // pin to an exact format because the registry's help renderer
        // (`registry.getHelp`) may evolve — what matters is that detailed
        // help is materially different from the unfiltered list.
        expect(text.toLowerCase()).toContain('overview');

    });

});

describe('mcp-discovery: run_noorm_cmd', () => {

    it('returns the expected object counts for `overview` against dev', async () => {

        const result = await client.callTool({
            name: 'run_noorm_cmd',
            arguments: { command: 'overview' },
        });

        expect(isError(result)).toBeFalsy();

        const payload = JSON.parse(firstText(result));

        // Counts are pinned to the schema defined in `sql/`. A drift here
        // means the build emitted unexpected objects — that's a real signal,
        // not noise. Update both sides intentionally if the schema grows.
        expect(payload).toMatchObject({
            tables: 38,
            views: 18,
            procedures: 72,
            functions: 14,
            types: 4,
        });

    });

    it('lists the four composite TVP types via `list { category: "types" }`', async () => {

        const result = await client.callTool({
            name: 'run_noorm_cmd',
            arguments: { command: 'list', payload: { category: 'types' } },
        });

        expect(isError(result)).toBeFalsy();

        const payload = JSON.parse(firstText(result));

        expect(Array.isArray(payload)).toBe(true);
        expect(payload).toHaveLength(4);

        const names = payload.map((t: { name: string }) => t.name).sort();

        expect(names).toEqual([
            'MemoryIdSet',
            'TagAttachmentInput',
            'TagIdSet',
            'TaskDependencyInput',
        ]);

    });

});

describe('mcp-discovery: error resilience', () => {

    it('flags a bad command with isError and keeps the server alive', async () => {

        const bad = await client.callTool({
            name: 'run_noorm_cmd',
            arguments: { command: 'nonexistent.command' },
        });

        // The server must mark the tool call as an error AND it must not
        // crash. Per `src/mcp/server.ts:38`, unknown commands return an
        // `isError: true` envelope with an "Unknown command" message —
        // exfiltrating the available command list so the caller can self-correct.
        expect(isError(bad)).toBe(true);

        const text = firstText(bad);
        const errorPayload = JSON.parse(text);

        expect(errorPayload.error).toMatch(/Unknown command/i);

        // The server must still answer follow-up calls. If the bad call
        // had crashed the subprocess, this would either throw (transport
        // closed) or hang past the test timeout.
        const recovered = await client.callTool({ name: 'noorm_help', arguments: {} });

        expect(isError(recovered)).toBeFalsy();

        const recoveredText = firstText(recovered);
        expect(recoveredText).toContain('**connect**');

    });

});
