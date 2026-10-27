/**
 * Waiting for the box to come back (docs/DESIGN.md "Control panel": "Rebooting, back in about 40s").
 * The page polls /healthz instead of /api/status, because the question is "is anything answering yet"
 * and a launcher that has not reloaded its state would answer that with a stale picture.
 */

/** The backoff ladder: 2s, 4s, 8s, 16s, then every 30s until it answers. */
export const HEALTH_BACKOFF_MS = [2_000, 4_000, 8_000, 16_000, 30_000] as const;

/** How long to wait before the next check, given how many have already failed. */
export function backoffDelay(attempt: number): number {
	const index = Math.min(Math.max(attempt, 0), HEALTH_BACKOFF_MS.length - 1);
	return HEALTH_BACKOFF_MS[index];
}

export type WatchOptions = {
	/** One health check: true when the hub answered. Injectable so a test needs no server. */
	probe: () => Promise<boolean>;
	/** Called after every failed check, with how many have failed so far. */
	onAttempt: (attempt: number, delay: number) => void;
	/** Called once, when a check succeeds. */
	onRecovered: () => void;
	setTimer?: (callback: () => void, delay: number) => number;
	clearTimer?: (handle: number) => void;
};

/**
 * Polls until the hub answers, then stops. It returns its own stop function, so a page that is
 * unmounted mid-reboot does not keep someone's phone awake.
 */
export function watchForRecovery(options: WatchOptions): () => void {
	const setTimer =
		options.setTimer ?? ((callback, delay) => setTimeout(callback, delay) as unknown as number);
	const clearTimer = options.clearTimer ?? ((handle) => clearTimeout(handle));

	let attempt = 0;
	let timer: number | null = null;
	let isStopped = false;

	function stop(): void {
		isStopped = true;
		if (timer !== null) {
			clearTimer(timer);
			timer = null;
		}
	}

	async function check(): Promise<void> {
		timer = null;
		if (isStopped) return;
		if (await options.probe()) {
			if (isStopped) return;
			options.onRecovered();
			return;
		}
		const delay = backoffDelay(attempt);
		attempt += 1;
		options.onAttempt(attempt, delay);
		if (isStopped) return;
		timer = setTimer(() => void check(), delay);
	}

	void check();
	return stop;
}
