import type { RunState } from './state';

// The stub upstream (testdata/stub) is the only thing in the suite that changes state: it stands in
// for the two services the portable fixture probes over HTTP. These two calls are all the specs need
// to move it.

/**
 * Takes one stub service up or down. The stub answers with the state of both, which is the check
 * that the flip landed rather than an assumption that it did.
 */
export async function flip(run: RunState, service: string, state: 'up' | 'down'): Promise<void> {
	const response = await fetch(`${run.stubURL}/control/${service}/${state}`, { method: 'POST' });
	if (!response.ok) {
		throw new Error(`the stub refused ${service} ${state}: ${response.status}`);
	}
}

/**
 * Asks the binary to probe everything now and waits for the results, which is the freshness path a
 * page load takes (`?fresh=1`). The suite uses it after restoring a service, so the next spec starts
 * from a settled page instead of waiting out a poll cycle for state it already knows.
 */
export async function refresh(run: RunState): Promise<void> {
	const response = await fetch(`${run.baseURL}/api/status?fresh=1`, { cache: 'no-store' });
	if (!response.ok) {
		throw new Error(`the binary refused a forced refresh: ${response.status}`);
	}
}
