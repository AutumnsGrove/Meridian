import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';
import { parseControlStatus } from './control';

// The golden is the same file the Go tests write (decision #59), so a field the container stops
// sending fails here before it fails on a phone.
const golden = JSON.parse(
	readFileSync(resolve(process.cwd(), '../testdata/control/status.golden.json'), 'utf8')
) as unknown;

function mutated(change: (document: Record<string, unknown>) => void): unknown {
	const copy = structuredClone(golden) as Record<string, unknown>;
	change(copy);
	return copy;
}

describe('parseControlStatus', () => {
	test('accepts the golden the Go side writes', () => {
		const result = parseControlStatus(golden);

		if (!result.ok) throw new Error(result.error);
		expect(result.value.executor.state).toBe('missing');
		expect(result.value.pending).toHaveLength(1);
		expect(result.value.results[0].sentence).toBe('That target does not allow this action.');
		expect(result.value.autorestore[0].remaining_s).toBe(540);
	});

	test('keeps an absent executor since as null rather than inventing a time', () => {
		const withoutSince = mutated((document) => {
			const executor = document.executor as Record<string, unknown>;
			delete executor.since;
		});

		const result = parseControlStatus(withoutSince);

		if (!result.ok) throw new Error(result.error);
		expect(result.value.executor.since).toBeNull();
	});

	test('accepts an empty control plane: no pending, no results, no restore', () => {
		const empty = mutated((document) => {
			document.pending = [];
			document.results = [];
			document.audit = [];
			document.autorestore = [];
		});

		const result = parseControlStatus(empty);

		if (!result.ok) throw new Error(result.error);
		expect(result.value.pending).toEqual([]);
	});

	test('refuses an executor state outside the closed set', () => {
		const wrongState = mutated((document) => {
			(document.executor as Record<string, unknown>).state = 'running';
		});

		const result = parseControlStatus(wrongState);

		expect(result.ok).toBe(false);
		if (!result.ok) expect(result.error).toContain('executor.state');
	});

	test('refuses a document that is not an object, or a list that is not a list', () => {
		expect(parseControlStatus(null).ok).toBe(false);
		expect(parseControlStatus([]).ok).toBe(false);
		expect(parseControlStatus(mutated((document) => (document.pending = {}))).ok).toBe(false);
	});

	test('names the field that failed, so a broken contract is debuggable', () => {
		const badRestore = mutated((document) => {
			(document.autorestore as Record<string, unknown>[])[0].remaining_s = '540';
		});

		const result = parseControlStatus(badRestore);

		expect(result.ok).toBe(false);
		if (!result.ok) expect(result.error).toContain('autorestore.0.remaining_s');
	});
});
