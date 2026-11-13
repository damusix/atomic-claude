import { appendFileSync } from "node:fs";

type SessionEvent = { reason?: unknown };
type SessionContext = { cwd?: unknown };
type ExtensionApi = {
	on(event: string, handler: (event: SessionEvent, ctx: SessionContext) => unknown): void;
};

export default function eventProbe(pi: ExtensionApi): void {
	const path = process.env.ATOMIC_OMP_EVENT_PROBE;
	if (!path) return;
	for (const name of ["session_start", "session_switch", "before_agent_start"]) {
		pi.on(name, (event, ctx) => {
			appendFileSync(path, JSON.stringify({ event: name, reason: event?.reason ?? "", cwd: ctx?.cwd ?? "" }) + "\n");
		});
	}
}
