import type { ParseResult } from '#lib/api/parse';
import type { Status } from '#lib/api/types';

/** How long a visible page waits after a good answer before asking again. */
export const POLL_INTERVAL_MS = 15_000;

/** How long it waits after a failure: slower, so a box that is down is not hammered. */
export const RETRY_INTERVAL_MS = 60_000;

export type PollState = {
	/** The last response that passed validation, kept across failures. */
	status: Status | null;
	/** When that response arrived, in epoch milliseconds. */
	lastGoodAt: number | null;
	/** The last failure, cleared by the next success. */
	error: string | null;
	isLoading: boolean;
	/** How long until the next request, set by the last outcome. */
	retryDelay: number;
};

export type PollEvent =
	| { type: 'requested' }
	| { type: 'succeeded'; status: Status; at: number }
	| { type: 'failed'; error: string };

export function initialPollState(): PollState {
	return {
		status: null,
		lastGoodAt: null,
		error: null,
		isLoading: false,
		retryDelay: POLL_INTERVAL_MS
	};
}

/**
 * The poll loop's state machine. A success replaces the data and clears the error; a failure keeps
 * the last good data (so the page still shows something) and moves to the slower interval.
 */
export function reducePoll(state: PollState, event: PollEvent): PollState {
	switch (event.type) {
		case 'requested':
			return state.isLoading ? state : { ...state, isLoading: true };
		case 'succeeded':
			return {
				status: event.status,
				lastGoodAt: event.at,
				error: null,
				isLoading: false,
				retryDelay: POLL_INTERVAL_MS
			};
		case 'failed':
			return { ...state, error: event.error, isLoading: false, retryDelay: RETRY_INTERVAL_MS };
	}
}

export type PollOptions = {
	/** The request itself; injectable so the loop can be tested without a server. */
	fetchStatus: () => Promise<ParseResult>;
	onChange: (state: PollState) => void;
	now?: () => number;
	isVisible?: () => boolean;
	setTimer?: (callback: () => void, delay: number) => number;
	clearTimer?: (handle: number) => void;
};

/**
 * Runs the poll loop until the returned function is called: one request on start, then one every
 * 15s while the page is visible, 60s after a failure, and an immediate one whenever the page comes
 * back to the foreground. It never has two requests in flight, and a stopped loop ignores a
 * response that arrives late.
 */
export function startPolling(options: PollOptions): () => void {
	const now = options.now ?? Date.now;
	const isVisible = options.isVisible ?? (() => document.visibilityState !== 'hidden');
	const setTimer =
		options.setTimer ?? ((callback, delay) => setTimeout(callback, delay) as unknown as number);
	const clearTimer = options.clearTimer ?? ((handle) => clearTimeout(handle));

	let state = initialPollState();
	let timer: number | null = null;
	let isStopped = false;
	let isInFlight = false;

	function announce(next: PollState): void {
		state = next;
		options.onChange(state);
	}

	function stopTimer(): void {
		if (timer === null) return;
		clearTimer(timer);
		timer = null;
	}

	function schedule(delay: number): void {
		stopTimer();
		if (isStopped) return;
		timer = setTimer(() => void poll(), delay);
	}

	async function poll(): Promise<void> {
		timer = null;
		if (isStopped || isInFlight) return;
		if (!isVisible()) {
			schedule(POLL_INTERVAL_MS);
			return;
		}

		isInFlight = true;
		announce(reducePoll(state, { type: 'requested' }));
		const result = await options.fetchStatus();
		isInFlight = false;
		if (isStopped) return;

		if (result.ok) {
			announce(reducePoll(state, { type: 'succeeded', status: result.value, at: now() }));
		} else {
			announce(reducePoll(state, { type: 'failed', error: result.error }));
		}
		schedule(state.retryDelay);
	}

	function onVisibilityChange(): void {
		if (!isVisible()) return;
		stopTimer();
		void poll();
	}

	document.addEventListener('visibilitychange', onVisibilityChange);
	options.onChange(state);
	void poll();

	return () => {
		isStopped = true;
		stopTimer();
		document.removeEventListener('visibilitychange', onVisibilityChange);
	};
}
