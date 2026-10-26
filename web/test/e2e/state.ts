import { readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * What the end-to-end run is: the app's address, the stub's address and the config it was started
 * with. The ports are chosen per run, so nothing here can be a constant.
 */
export type RunState = {
	/** The fixture's app, as the browser reaches it. */
	baseURL: string;
	/** The stub upstream the fixture's HTTP probes point at. */
	stubURL: string;
	/** The config the binary was started with, for a failure message worth reading. */
	configPath: string;
};

/**
 * Where the global setup leaves that state. The path is fixed rather than passed along because a
 * Playwright config is evaluated before `globalSetup` runs while the specs are loaded after it: the
 * setup writes, the specs read, and neither needs the other's process or environment.
 */
export const statePath = join(tmpdir(), 'meridian-e2e', 'state.json');

/** Reads the state the global setup wrote, with the instruction a person needs if it is missing. */
export function readRunState(): RunState {
	let raw: string;
	try {
		raw = readFileSync(statePath, 'utf8');
	} catch {
		throw new Error(`no e2e state at ${statePath}: run scripts/check e2e, which starts the binary`);
	}
	return JSON.parse(raw) as RunState;
}
