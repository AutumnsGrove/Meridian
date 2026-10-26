import { loadStatus, serviceNamed } from '../../../test/golden';
import { describe, expect, test } from 'vitest';
import type { PollState } from './poller';
import { initialPollState } from './poller';
import { bannerText, restartHref, statusWord, summaryLabel, vitalText, vitalTone } from './status';
import { SERVICE_STATUSES } from '#lib/api/types';

const status = loadStatus('status.all-ok.golden.json');
const now = Date.parse(status.generated_at);

describe('statusWord', () => {
	test.each([
		{ status: 'ok', want: 'healthy' },
		{ status: 'degraded', want: 'degraded' },
		{ status: 'down', want: 'down' },
		{ status: 'unknown', want: 'unknown' },
		{ status: 'disabled', want: 'disabled' }
	] as const)('speaks $status as $want, not as colour alone', ({ status, want }) => {
		expect(statusWord(status)).toBe(want);
	});

	test('has a word for every status in the closed set', () => {
		for (const status of SERVICE_STATUSES) {
			expect(statusWord(status)).not.toBe('');
		}
	});
});

describe('summaryLabel', () => {
	test('is the up count over the total, as the server counted them', () => {
		expect(summaryLabel(status)).toBe('4/4 up');
	});
});

describe('restartHref', () => {
	test('a down service with a restart action links to its control row', () => {
		const gitea = serviceNamed(status, 'Gitea');

		expect(restartHref({ ...gitea, status: 'down' })).toBe('/control#gitea');
	});

	test('a healthy service has no restart affordance, even when it could be restarted', () => {
		expect(restartHref(serviceNamed(status, 'Gitea'))).toBeNull();
	});

	test('a down service the control plane cannot restart has none either', () => {
		const down = loadStatus('status.one-down.golden.json');
		const jellyfin = serviceNamed(down, 'Jellyfin');
		const noRestart = {
			target: 'jellyfin',
			actions: ['stop'],
			update_disabled_reason: '',
			dns: false
		};

		expect(restartHref({ ...jellyfin, control: noRestart })).toBeNull();
		expect(restartHref({ ...jellyfin, control: null })).toBeNull();
	});
});

describe('vitalTone', () => {
	const gitea = serviceNamed(status, 'Gitea');
	const vital = gitea.vitals[0];
	const agedBy = (seconds: number) => ({
		...vital,
		at: new Date(now - seconds * 1000).toISOString()
	});

	test('a fresh, passing vital is normal', () => {
		expect(vitalTone(agedBy(5), now)).toBe('normal');
	});

	test('a failing vital is failed even when it is the newest reading', () => {
		expect(vitalTone({ ...agedBy(0), failing: true }, now)).toBe('failed');
	});

	test('a passing vital older than five minutes is stale', () => {
		expect(vitalTone(agedBy(300), now)).toBe('normal');
		expect(vitalTone(agedBy(301), now)).toBe('stale');
	});

	test('dims on the same clock the label is drawn from, so the two cannot disagree', () => {
		const fiveMinutes = agedBy(301);

		expect(vitalText(fiveMinutes, now)).toBe('1.22.0 · 5m ago');
		expect(vitalTone(fiveMinutes, now)).toBe('stale');
	});

	test('a timestamp the browser cannot read counts as fresh, never as stale', () => {
		expect(vitalTone({ ...vital, at: 'not a timestamp' }, now)).toBe('normal');
	});

	test('a reading from the future is fresh, so clock skew does not dim the page', () => {
		expect(vitalTone(agedBy(-60), now)).toBe('normal');
	});
});

describe('vitalText', () => {
	const vital = { label: 'peers', value: '3 online', at: '', age_s: 5, failing: false };

	test('shows the value alone while it is fresh', () => {
		expect(vitalText({ ...vital, at: new Date(now - 5000).toISOString() }, now)).toBe('3 online');
	});

	test('adds how old the reading is once it is a minute old', () => {
		expect(vitalText({ ...vital, at: new Date(now - 120_000).toISOString() }, now)).toBe(
			'3 online · 2m ago'
		);
	});
});

describe('bannerText', () => {
	const good = { ...initialPollState(), lastGoodAt: now - 120_000 };

	test('says nothing while the last request succeeded', () => {
		expect(bannerText({ ...good, status }, now)).toBeNull();
	});

	test('says which data is still on screen after a failure', () => {
		expect(bannerText({ ...good, status, error: 'status 502' } as PollState, now)).toBe(
			"Can't reach Meridian, showing data from 2m ago"
		);
	});

	test('says only that it is unreachable before the first answer', () => {
		expect(bannerText({ ...initialPollState(), error: 'Failed to fetch' }, now)).toBe(
			"Can't reach Meridian"
		);
	});
});
