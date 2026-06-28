/**
 * Layer 3 — observer integration tests.
 *
 * Asserts that ctx.noorm.observer surfaces the cross-cutting events
 * emitted by the runner and template engine while the SDK executes
 * real SQL against the test MSSQL database.
 */
import { join } from 'node:path';

import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'bun:test';
import { attempt } from '@logosdx/utils';

import { bootstrap, resetApplicationData } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await resetApplicationData(ctx);

});

describe('observer: file:* events during run.file', () => {

    it('emits file:before and file:after when running a .sql.tmpl', async () => {

        const events: { event: string; filepath: string }[] = [];

        const cleanup = ctx.noorm.observer.on(/^file:/, (payload) => {

            const evt = payload.event;
            const data: unknown = payload.data;

            if (typeof evt !== 'string') return;
            if (typeof data !== 'object' || data === null) return;

            if (!('filepath' in data)) return;
            const fp = data.filepath;
            if (typeof fp !== 'string') return;

            events.push({ event: evt, filepath: fp });

        });

        await ctx.noorm.run.file('sql/06_seeds/11_Sentinels.sql.tmpl', { force: true });
        cleanup();

        const before = events.find((e) => e.event === 'file:before' && e.filepath.endsWith('11_Sentinels.sql.tmpl'));
        const after = events.find((e) => e.event === 'file:after' && e.filepath.endsWith('11_Sentinels.sql.tmpl'));

        if (!before) throw new Error(`no file:before event captured; got: ${JSON.stringify(events)}`);
        if (!after) throw new Error(`no file:after event captured; got: ${JSON.stringify(events)}`);

        expect(before.event).toBe('file:before');
        expect(after.event).toBe('file:after');

    });

    it('file:after carries status and durationMs for a successful run', async () => {

        const captured: { status: string; durationMs: number; filepath: string }[] = [];

        const cleanup = ctx.noorm.observer.on('file:after', (data) => {

            captured.push({
                status: data.status,
                durationMs: data.durationMs,
                filepath: data.filepath,
            });

        });

        await ctx.noorm.run.file('sql/06_seeds/11_Sentinels.sql.tmpl', { force: true });
        cleanup();

        const matched = captured.find((e) => e.filepath.endsWith('11_Sentinels.sql.tmpl'));
        if (!matched) throw new Error(`no file:after for sentinel seed; got: ${JSON.stringify(captured)}`);

        expect(matched.status).toBe('success');
        expect(matched.durationMs).toBeGreaterThanOrEqual(0);

    });

});

describe('observer: template:* events during .sql.tmpl render', () => {

    it('emits template:render when a templated file is executed', async () => {

        const renders: { filepath: string; durationMs: number }[] = [];

        const cleanup = ctx.noorm.observer.on('template:render', (data) => {

            renders.push({ filepath: data.filepath, durationMs: data.durationMs });

        });

        await ctx.noorm.run.file('sql/06_seeds/11_Sentinels.sql.tmpl', { force: true });
        cleanup();

        const matched = renders.find((e) => e.filepath.endsWith('11_Sentinels.sql.tmpl'));
        if (!matched) throw new Error(`no template:render captured; got: ${JSON.stringify(renders)}`);

        expect(matched.durationMs).toBeGreaterThanOrEqual(0);

    });

});

describe('observer: regex pattern matching across run.file', () => {

    it('captures every file:* event under a single /^file:/ subscription', async () => {

        const events: string[] = [];

        const cleanup = ctx.noorm.observer.on(/^file:/, (payload) => {

            const evt = payload.event;
            if (typeof evt === 'string') events.push(evt);

        });

        await ctx.noorm.run.file('sql/06_seeds/11_Sentinels.sql.tmpl', { force: true });
        cleanup();

        expect(events.length).toBeGreaterThanOrEqual(2);

        for (const name of events) {

            expect(name.startsWith('file:')).toBe(true);

        }

    });

});

// Per `mssql-problems.md` gap #4, runner failure paths historically
// emitted file:after with sparse error detail (CLI/JSON envelope drops
// the message). The SDK path through `noorm.run.file()` does capture
// `getSqlErrorMessage(execErr)` into both the returned FileResult and
// the `file:after` event payload (runner.ts:910 / runner.ts:932), so we
// can assert on observable shape here without relying on stdout/JSON.
//
// We therefore assert two independent error signals so the test still
// passes if one weakens: (a) `run.file()` resolves with a failed
// FileResult, and (b) a `file:after` event with status !== 'success'
// is observed.
describe('observer: error path', () => {

    const BAD_FILE_REL = 'tmp/observer-bad.sql';
    const BAD_FILE_ABS = join(import.meta.dir, '..', '..', BAD_FILE_REL);

    beforeAll(async () => {

        // Intentionally invalid T-SQL — the keyword is misspelled, so
        // tedious will reject the statement. No GO batches (gap #3).
        await Bun.write(BAD_FILE_ABS, 'SELEKT 1;\n');

    });

    afterAll(async () => {

        // Clean up the temp file. Use attempt() because Bun.file().unlink()
        // is the documented removal API but we don't want a missing-file
        // error in cleanup to mask a real test failure.
        await attempt(() => Bun.file(BAD_FILE_ABS).unlink());

    });

    it('emits a file:after event with a non-success status when SQL execution fails', async () => {

        const captured: { filepath: string; status: string; error: string | undefined }[] = [];

        const cleanup = ctx.noorm.observer.on('file:after', (data) => {

            captured.push({
                filepath: data.filepath,
                status: data.status,
                error: data.error,
            });

        });

        // run.file() returns a FileResult — failure does not throw. Wrap
        // anyway so a future SDK that throws is also handled.
        const [result, threwErr] = await attempt(
            () => ctx.noorm.run.file(BAD_FILE_REL, { force: true }),
        );
        cleanup();

        // Either path is acceptable evidence of a failure signal:
        //  (1) the call threw, or
        //  (2) the FileResult reports status: 'failed'.
        const failedResult = result && result.status !== 'success';
        const threwOrFailed = Boolean(threwErr) || Boolean(failedResult);

        expect(threwOrFailed).toBe(true);

        // And — independently — we should see an event for the bad file.
        const matched = captured.find((e) => e.filepath.endsWith('observer-bad.sql'));
        if (!matched) {

            throw new Error(`no file:after event captured for bad file; got: ${JSON.stringify(captured)}`);

        }

        // Per gap #4, the `error` payload may be sparse on some pipelines.
        // Assert only what we are sure about: status reflects failure.
        expect(matched.status).not.toBe('success');

    });

});
