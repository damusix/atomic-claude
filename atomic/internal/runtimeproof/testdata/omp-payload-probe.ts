import { appendFileSync } from "node:fs";

type ProviderRequestEvent = { payload?: unknown };
type ExtensionApi = {
	on(event: "before_provider_request", handler: (event: ProviderRequestEvent) => unknown): void;
};

export default function payloadProbe(pi: ExtensionApi): void {
	pi.on("before_provider_request", (event) => {
		const path = process.env.ATOMIC_OMP_PAYLOAD_PROBE;
		if (!path) return;
		appendFileSync(path, JSON.stringify(event?.payload ?? null) + "\n");
	});
}
