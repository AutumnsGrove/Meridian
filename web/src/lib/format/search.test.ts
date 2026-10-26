import fc from 'fast-check';
import { describe, expect, test } from 'vitest';
import { buildSearchURL } from './search';

const template = 'https://search.example.org/?q={q}';

describe('buildSearchURL', () => {
	test('puts the query where the placeholder was', () => {
		expect(buildSearchURL(template, 'compost')).toBe('https://search.example.org/?q=compost');
	});

	test('percent-encodes what would otherwise change the URL', () => {
		expect(buildSearchURL(template, 'a b')).toBe('https://search.example.org/?q=a%20b');
		expect(buildSearchURL(template, 'a&b')).toBe('https://search.example.org/?q=a%26b');
		expect(buildSearchURL(template, 'a#b')).toBe('https://search.example.org/?q=a%23b');
		expect(buildSearchURL(template, 'a"b')).toBe('https://search.example.org/?q=a%22b');
	});

	test('leaves the rest of the template untouched', () => {
		const withPath = 'https://search.example.org/find/{q}?lang=en';

		expect(buildSearchURL(withPath, 'ivy')).toBe('https://search.example.org/find/ivy?lang=en');
	});

	const property = fc.property(fc.string(), (query) => {
		const url = buildSearchURL(template, query);

		if (!url.startsWith('https://search.example.org/?q=')) return false;
		// The bare query must never smuggle a second parameter, a fragment or a quote into the URL.
		if (/[ #&"]/.test(url.slice('https://search.example.org/?q='.length))) return false;
		return new URL(url).searchParams.get('q') === query;
	});

	test('the result always opens the configured engine with exactly the query asked for', () => {
		fc.assert(property, { numRuns: 300 });
	});
});
