import { mkdir, unlink, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

describe('ctx.noorm.observer file:after', () => {

    it('fires with filepath, status, and durationMs when a file is run', async () => {

        const events: Array<{ filepath: string; status: string; durationMs: number }> = [];

        const cleanup = ctx.noorm.observer.on('file:after', (data) => {

            events.push({
                filepath:   data.filepath,
                status:     data.status,
                durationMs: data.durationMs,
            });

        });

        await ctx.noorm.run.file('sql/05_seeds/03_sentinel_rows.sql.tmpl');
        cleanup();

        const matched = events.find((e) => e.filepath.endsWith('03_sentinel_rows.sql.tmpl'));
        if (!matched) throw new Error(`no file:after event for sentinel seed; got: ${JSON.stringify(events)}`);

        expect(matched.status).toBe('success');
        expect(matched.durationMs).toBeGreaterThanOrEqual(0);

    });

});

describe('ctx.noorm.observer change:complete', () => {

    it('fires when a freshly created change is applied via the SDK', async () => {

        const events: Array<{ name: string; direction: string; status: string; durationMs: number }> = [];

        const cleanup = ctx.noorm.observer.on('change:complete', (data) => {

            events.push({
                name:       data.name,
                direction:  data.direction,
                status:     data.status,
                durationMs: data.durationMs,
            });

        });

        // Use a unique-ish description so we don't collide with the existing
        // 2026-05-10-add-memory-tag-color change in changes/.
        const description = `observer-test-${Date.now().toString(36)}`;
        const change = await ctx.noorm.changes.create({ description });

        // A change must contain at least one file in change/ or revert/ to be
        // applicable. Add a no-op SQL file so apply() has something to run.
        const withFile = await ctx.noorm.changes.addFile(change, 'change', {
            name: 'noop',
            type: 'sql',
            content: 'SELECT 1;\n',
        });

        // Re-acquire to confirm the change is on disk and parseable.
        const parsed = await ctx.noorm.changes.parse(withFile.name);

        const result = await ctx.noorm.changes.apply(parsed.name);
        cleanup();

        // Cleanup the scaffolded change directory regardless of test outcome.
        await ctx.noorm.changes.delete(parsed);

        expect(result).toBeDefined();

        const matched = events.find((e) => e.name === parsed.name && e.direction === 'change');
        if (!matched) throw new Error(`no change:complete event for ${parsed.name}; got: ${JSON.stringify(events)}`);

        expect(matched.status).toBe('success');
        expect(matched.durationMs).toBeGreaterThanOrEqual(0);

    });

});

describe('ctx.noorm.observer regex pattern matching', () => {

    it('routes multiple file:* events through a /^file:/ subscription during run.dir', async () => {

        const fileEvents: string[] = [];

        const cleanup = ctx.noorm.observer.on(/^file:/, (payload) => {

            // RegExp listeners receive { event, data }.
            const evt = payload.event;
            if (typeof evt === 'string') fileEvents.push(evt);

        });

        await ctx.noorm.run.dir('sql/05_seeds');
        cleanup();

        // Every event captured should be in the file:* namespace.
        for (const name of fileEvents) {

            expect(name.startsWith('file:')).toBe(true);

        }

        // The /^file:/ subscription must route BOTH lifecycle phases — a
        // before/after pair per file proves the regex matches multiple
        // distinct event names, not just one repeating event.
        const distinctEventNames = new Set(fileEvents);
        expect(distinctEventNames.has('file:before')).toBe(true);
        expect(distinctEventNames.has('file:after')).toBe(true);

        // Lifecycle is balanced: every file that emits before must emit after.
        const beforeCount = fileEvents.filter((n) => n === 'file:before').length;
        const afterCount  = fileEvents.filter((n) => n === 'file:after').length;
        expect(beforeCount).toBeGreaterThan(0);
        expect(afterCount).toBe(beforeCount);

    });

});

describe('ctx.noorm.observer file:after status=failed', () => {

    it('emits file:after with status="failed" when the executed SQL throws', async () => {

        const events: Array<{ filepath: string; status: string; error?: string }> = [];

        const cleanup = ctx.noorm.observer.on('file:after', (data) => {

            events.push({
                filepath: data.filepath,
                status:   data.status,
                error:    data.error,
            });

        });

        // Write a temp .sql file with deliberately broken SQL, run it, and
        // verify that file:after fires with status="failed".
        //
        // Note: ctx.noorm.run.file() resolves with a result object even on
        // SQL failure — the failure surfaces via the file:after event's
        // status field, not via a thrown error. This is intentional in
        // noorm: callers subscribe to events for granular feedback.
        const tmpDir = join(process.cwd(), 'tmp');
        await mkdir(tmpDir, { recursive: true });
        const badSqlPath = join(tmpDir, '__observer_bad_sql__.sql');
        await writeFile(badSqlPath, 'THIS IS NOT VALID SQL;\n');

        const result = await ctx.noorm.run.file(badSqlPath);

        cleanup();

        // The runner emits file:after with status="failed" for the bad file.
        const matchingEvents = events.filter((e) => e.filepath.includes('__observer_bad_sql__'));

        expect(matchingEvents.length).toBeGreaterThan(0);

        for (const e of matchingEvents) {

            expect(e.status).toBe('failed');

        }

        // The returned result object should also reflect failure.
        expect(result.status).toBe('failed');

        await unlink(badSqlPath).catch(() => {});

    });

});
