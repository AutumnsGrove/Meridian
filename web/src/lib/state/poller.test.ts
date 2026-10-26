import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import type { ParseResult } from '#lib/api/parse';
import { loadStatus } from '../../../test/golden';
import {
	POLL_INTERVAL_MS,
	RETRY_INTERVAL_MS,
	initialPollState,
	reducePoll,
	startPolling
} from './poller';

const status = loadStatus('status.all-ok.golden.json');
const ok: ParseResult = { ok: true, value: status };

describe('poll intervals', () => {
	test('are the ones the design names: every 15s while visible, 60s after a failure', () => {
		expect(POLL_INTERVAL_MS).toBe(15_000);
		expect(RETRY_INTERVAL_MS).toBe(60_000);
	});
});

describe('reducePoll', () => {
	test('starts with no data and no error', () => {
		expect(initialPollState()).toEqual({
			status: null,
			lastGoodAt: null,
			error: null,
			isLoading: false,
			retryDelay: POLL_INTERVAL_MS
		});
	});

	test('a request in flight is marked loading, once', () => {
		const loading = reducePoll(initialPollState(), { type: 'requested' });

		expect(loading.isLoading).toBe(true);
		expect(reducePoll(loading, { type: 'requested' })).toBe(loading);
	});

	test('a success stores the data, the time and the fast interval', () => {
		const failed = reducePoll(initialPollState(), { type: 'failed', error: 'boom' });

		const state = reducePoll(failed, { type: 'succeeded', status, at: 1000 });

		expect(state).toMatchObject({
			status,
			lastGoodAt: 1000,
			error: null,
			isLoading: false,
			retryDelay: POLL_INTERVAL_MS
		});
	});

	test('a failure keeps the last good data and moves to the slow interval', () => {
		const good = reducePoll(initialPollState(), { type: 'succeeded', status, at: 1000 });

		const state = reducePoll(good, { type: 'failed', error: 'status 502' });

		expect(state).toMatchObject({
			status,
			lastGoodAt: 1000,
			error: 'status 502',
			isLoading: false,
			retryDelay: RETRY_INTERVAL_MS
		});
	});
});

describe('startPolling', () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	const requests = (fn: () => Promise<ParseResult>) => {
		const called = vi.fn(fn);
		return called;
	};

	test('asks once when it starts, then every 15s while the page is visible', async () => {
		const responses = [ok, ok, ok];
		const fetchStatus = requests(async () => responses.shift() ?? ok);
		const states: unknown[] = [];

		const stop = startPolling({ fetchStatus, onChange: (state) => states.push(state) });
		await vi.advanceTimersByTimeAsync(0);
		expect(fetchStatus).toHaveBeenCalledTimes(1);

		await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS - 1);
		expect(fetchStatus).toHaveBeenCalledTimes(1);

		await vi.advanceTimersByTimeAsync(1);
		expect(fetchStatus).toHaveBeenCalledTimes(2);

		stop();
		expect(states.length).toBeGreaterThan(0);
	});

	test('keeps the last good data on failure and waits a minute before trying again', async () => {
		let answer: ParseResult = ok;
		const fetchStatus = requests(async () => answer);
		let latest = initialPollState();

		const stop = startPolling({
			fetchStatus,
			now: () => 5_000,
			onChange: (state) => (latest = state)
		});
		await vi.advanceTimersByTimeAsync(0);
		expect(latest.status).toBe(status);

		answer = { ok: false, error: 'status 502' };
		await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
		expect(fetchStatus).toHaveBeenCalledTimes(2);
		expect(latest).toMatchObject({
			status,
			lastGoodAt: 5_000,
			error: 'status 502',
			retryDelay: RETRY_INTERVAL_MS
		});

		await vi.advanceTimersByTimeAsync(RETRY_INTERVAL_MS - 1);
		expect(fetchStatus).toHaveBeenCalledTimes(2);
		await vi.advanceTimersByTimeAsync(1);
		expect(fetchStatus).toHaveBeenCalledTimes(3);

		answer = ok;
		await vi.advanceTimersByTimeAsync(RETRY_INTERVAL_MS);
		expect(latest.error).toBeNull();
		expect(latest.retryDelay).toBe(POLL_INTERVAL_MS);

		stop();
	});

	test('waits without asking while the page is hidden, and asks at once when it returns', async () => {
		let visible = false;
		const fetchStatus = requests(async () => ok);

		const stop = startPolling({ fetchStatus, isVisible: () => visible, onChange: () => {} });
		await vi.advanceTimersByTimeAsync(0);
		expect(fetchStatus).not.toHaveBeenCalled();

		await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
		expect(fetchStatus).not.toHaveBeenCalled();

		visible = true;
		document.dispatchEvent(new Event('visibilitychange'));
		await vi.advanceTimersByTimeAsync(0);
		expect(fetchStatus).toHaveBeenCalledTimes(1);

		stop();
	});

	test('never has two requests in flight', async () => {
		let release: (result: ParseResult) => void = () => {};
		const fetchStatus = requests(
			() =>
				new Promise<ParseResult>((resolve) => {
					release = resolve;
				})
		);

		const stop = startPolling({ fetchStatus, onChange: () => {} });
		await vi.advanceTimersByTimeAsync(0);
		expect(fetchStatus).toHaveBeenCalledTimes(1);

		// A second request would be due now, but the first has not answered yet.
		await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 3);
		expect(fetchStatus).toHaveBeenCalledTimes(1);

		release(ok);
		await vi.advanceTimersByTimeAsync(0);
		await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
		expect(fetchStatus).toHaveBeenCalledTimes(2);

		stop();
	});

	test('a stopped loop neither asks again nor reports a late answer', async () => {
		let release: (result: ParseResult) => void = () => {};
		const fetchStatus = requests(
			() =>
				new Promise<ParseResult>((resolve) => {
					release = resolve;
				})
		);
		const states: unknown[] = [];

		const stop = startPolling({ fetchStatus, onChange: (state) => states.push(state) });
		await vi.advanceTimersByTimeAsync(0);
		const changesBeforeStop = states.length;

		stop();
		release(ok);
		await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS * 2);

		expect(fetchStatus).toHaveBeenCalledTimes(1);
		expect(states.length).toBe(changesBeforeStop);
	});
});
