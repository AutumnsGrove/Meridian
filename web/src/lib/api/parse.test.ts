import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import fc from 'fast-check';
import { describe, expect, test } from 'vitest';
import { parseStatus } from './parse';
import { SERVICE_STATUSES } from './types';

type Json = Record<string, unknown>;

// The golden files are the contract, shared with the Go tests (decision #59): the browser must
// accept exactly what the server sends, so these tests read the real files and not a copy.
// Vitest runs with web/ as the working directory, and the goldens live at the repo root.
const goldenDir = resolve(process.cwd(), '../testdata/api') + '/';
const goldenFiles = readdirSync(goldenDir).filter((name) => name.endsWith('.golden.json'));

function golden(name: string): Json {
	return JSON.parse(readFileSync(goldenDir + name, 'utf8'));
}

function firstService(doc: Json): Json {
	const sections = doc.sections as Json[];
	const services = sections[0].services as Json[];
	return services[0];
}

/** The portable fixture's Gitea: the one service with a vital and a control block. */
function serviceWithVitalsAndControl(doc: Json): Json {
	const sections = doc.sections as Json[];
	const services = sections[1].services as Json[];
	return services[0];
}

describe('parseStatus', () => {
	test.each(goldenFiles)('accepts the golden file %s', (name) => {
		const result = parseStatus(golden(name));

		expect(result).toMatchObject({ ok: true });
	});

	const mutations: Array<{ name: string; mutate: (doc: Json) => void }> = [
		{ name: 'no sections', mutate: (doc) => void delete doc.sections },
		{ name: 'no summary', mutate: (doc) => void delete doc.summary },
		{ name: 'no search', mutate: (doc) => void delete doc.search },
		{ name: 'no config', mutate: (doc) => void delete doc.config },
		{
			name: 'summary.up is a string',
			mutate: (doc) => void ((doc.summary as Json).up = '4')
		},
		{
			name: 'config.ok is not a boolean',
			mutate: (doc) => void ((doc.config as Json).ok = 'yes')
		},
		{
			name: 'status outside the closed set',
			mutate: (doc) => void (firstService(doc).status = 'broken')
		},
		{
			name: 'status is not a string',
			mutate: (doc) => void (firstService(doc).status = 3)
		},
		{
			name: 'vitals is not an array',
			mutate: (doc) => void (firstService(doc).vitals = 'none')
		},
		{
			name: 'a vital without a value',
			mutate: (doc) => {
				const vitals = serviceWithVitalsAndControl(doc).vitals as Json[];
				delete vitals[0].value;
			}
		},
		{
			name: 'control is neither null nor an object',
			mutate: (doc) => void (firstService(doc).control = 'yes')
		},
		{
			name: 'actions is not an array of strings',
			mutate: (doc) => {
				const control = serviceWithVitalsAndControl(doc).control as Json;
				control.actions = [1, 2];
			}
		},
		{
			name: 'icon names two sources',
			mutate: (doc) => void (firstService(doc).icon = { lucide: 'mail', image: '/icons/a.png' })
		},
		{ name: 'icon names none', mutate: (doc) => void (firstService(doc).icon = {}) },
		{
			name: 'checked_at is not a timestamp',
			mutate: (doc) => void (firstService(doc).checked_at = 0)
		},
		{ name: 'host is a string', mutate: (doc) => void (doc.host = 'now') },
		{
			name: 'a search button without a template',
			mutate: (doc) => {
				const search = doc.search as Json;
				(search.buttons as Json[])[0] = { label: 'Search' };
			}
		},
		{
			name: 'the whole response is an array',
			mutate: (doc) => {
				for (const key of Object.keys(doc)) delete doc[key];
				Object.assign(doc, []);
			}
		}
	];

	test.each(mutations)('refuses a response with $name', ({ mutate }) => {
		const doc = golden('status.all-ok.golden.json');
		mutate(doc);

		const result = parseStatus(doc);

		expect(result.ok).toBe(false);
		if (!result.ok) {
			expect(result.error).not.toBe('');
		}
	});

	test('accepts a null host and a null control, because both mean "nothing to show"', () => {
		const doc = golden('status.all-ok.golden.json');
		doc.host = null;
		firstService(doc).control = null;

		expect(parseStatus(doc).ok).toBe(true);
	});

	test('ignores fields it does not know, so the server can add one first', () => {
		const doc = golden('status.all-ok.golden.json');
		doc.tomorrow = { added: true };
		firstService(doc).future = 1;

		expect(parseStatus(doc).ok).toBe(true);
	});

	test('every accepted status is one of the five the design names', () => {
		for (const name of goldenFiles) {
			const result = parseStatus(golden(name));
			if (!result.ok) throw new Error(`${name}: ${result.error}`);
			for (const section of result.value.sections) {
				for (const service of section.services) {
					expect(SERVICE_STATUSES).toContain(service.status);
				}
			}
		}
	});

	test('never throws on arbitrary JSON, and accepts its own output', () => {
		fc.assert(
			fc.property(fc.jsonValue(), (value) => {
				const result = parseStatus(value);
				if (!result.ok) return typeof result.error === 'string' && result.error !== '';
				const again = parseStatus(result.value);
				return again.ok;
			}),
			{ numRuns: 200 }
		);
	});
});
