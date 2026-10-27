import { afterEach, describe, expect, test, vi } from 'vitest';
import { HEALTH_BACKOFF_MS, backoffDelay, watchForRecovery } from './reconnect';

afterEach(() => {
	vi.useRealTimers();
	vi.restoreAllMocks();
});

describe('backoffDelay', () => {
	test('walks 2s, 4s, 8s, 16s and then waits 30s', () => {
		expect([0, 1, 2, 3, 4, 5, 60].map(backoffDelay)).toEqual([
			2_000, 4_000, 8_000, 16_000, 30_000, 30_000, 30_000
		]);
	});

	test('never returns nothing, which would spin', () => {
		expect(backoffDelay(-1)).toBe(HEALTH_BACKOFF_MS[0]);
	});
});

describe('watchForRecovery', () => {
	test('keeps checking on the ladder and stops the moment the hub answers', async () => {
		vi.useFakeTimers();
		const answers = [false, false, true];
		const probe = vi.fn(async () => answers.shift() ?? false);
		const onAttempt = vi.fn();
		const onRecovered = vi.fn();

		const stop = watchForRecovery({ probe, onAttempt, onRecovered });
		await vi.advanceTimersByTimeAsync(0);
		expect(probe).toHaveBeenCalledTimes(1);

		await vi.advanceTimersByTimeAsync(HEALTH_BACKOFF_MS[0]);
		expect(probe).toHaveBeenCalledTimes(2);
		expect(onAttempt).toHaveBeenLastCalledWith(2, HEALTH_BACKOFF_MS[1]);

		await vi.advanceTimersByTimeAsync(HEALTH_BACKOFF_MS[1]);
		expect(probe).toHaveBeenCalledTimes(3);
		expect(onRecovered).toHaveBeenCalledTimes(1);

		// Recovered means done: nothing else is scheduled.
		await vi.advanceTimersByTimeAsync(60_000);
		expect(probe).toHaveBeenCalledTimes(3);
		stop();
	});

	test('a stopped watch ignores the answer that was already on its way', async () => {
		vi.useFakeTimers();
		let resolve: (value: boolean) => void = () => {};
		const probe = vi.fn(
			() =>
				new Promise<boolean>((done) => {
					resolve = done;
				})
		);
		const onRecovered = vi.fn();

		const stop = watchForRecovery({ probe, onAttempt: vi.fn(), onRecovered });
		stop();
		resolve(true);
		await vi.advanceTimersByTimeAsync(0);

		expect(onRecovered).not.toHaveBeenCalled();
	});
});
