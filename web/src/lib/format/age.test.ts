import { describe, expect, test } from 'vitest';
import { ageLabel } from './age';

const now = Date.parse('2026-10-25T12:00:00Z');
const at = (secondsAgo: number) => new Date(now - secondsAgo * 1000).toISOString();

describe('ageLabel', () => {
	test.each([
		{ name: 'zero seconds', seconds: 0, want: 'now' },
		{ name: 'just under the first minute', seconds: 44, want: 'now' },
		{ name: 'the first minute', seconds: 45, want: '1m' },
		{ name: 'just under two minutes', seconds: 119, want: '1m' },
		{ name: 'two minutes', seconds: 120, want: '2m' },
		{ name: 'just under an hour', seconds: 3599, want: '59m' },
		{ name: 'one hour', seconds: 3600, want: '1h' },
		{ name: 'just under a day', seconds: 86_399, want: '23h' },
		{ name: 'one day', seconds: 86_400, want: '1d' },
		{ name: 'a week', seconds: 7 * 86_400, want: '7d' }
	])('says $want for $seconds seconds', ({ seconds, want }) => {
		expect(ageLabel(at(seconds), now)).toBe(want);
	});

	test('a timestamp in the future says "now" instead of a negative age', () => {
		expect(ageLabel(at(-30), now)).toBe('now');
	});

	test('an unparseable timestamp says "now" rather than NaN', () => {
		expect(ageLabel('', now)).toBe('now');
		expect(ageLabel('yesterday', now)).toBe('now');
	});
});
