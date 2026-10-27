import type { ParseOutcome } from '#lib/api/fields';
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

/**
 * A loop over any document: the same "keep the last good value" rule as the launcher's, so the
 * control page does not need its own copy of the mechanics.
 */
export type LoopState<T> = {
	value: T | null;
	lastGoodAt: number | null;
	error: string | null;
	isLoading: boolean;
};

export type LoopEvent<T> =
	| { type: 'requested' }
	| { type: 'succeeded'; value: T; at: number }
	| { type: 'failed'; error: string };

export function initialLoopState<T>(): LoopState<T> {
	return { value: null, lastGoodAt: null, error: null, isLoading: false };
}

export function reduceLoop<T>(state: LoopState<T>, event: LoopEvent<T>): LoopState<T> {
	switch (event.type) {
		case 'requested':
			return state.isLoading ? state : { ...state, isLoading: true };
		case 'succeeded':
			return { value: event.value, lastGoodAt: event.at, error: null, isLoading: false };
		case 'failed':
			return { ...state, error: event.error, isLoading: false };
	}
}

/** What a started loop can do afterwards. */
export type Loop = {
	/** Stops the loop; a response that arrives later is ignored. */
	stop: () => void;
	/** Asks again at the next opportunity, e.g. right after the page queued something. */
	refresh: () => void;
};

export type LoopOptions<T> = {
	/** The request itself; injectable so the loop can be tested without a server. */
	request: () => Promise<ParseOutcome<T>>;
	/** How long to wait before asking again, from the state that was just announced. */
	nextDelay: (state: LoopState<T>) => number;
	onChange: (state: LoopState<T>) => void;
	now?: () => number;
	isVisible?: () => boolean;
	setTimer?: (callback: () => void, delay: number) => number;
	clearTimer?: (handle: number) => void;
};

/**
 * Runs one request on start, then one every `nextDelay(state)` while the page is visible, plus an
 * immediate one whenever it comes back to the foreground or `refresh` is called. It never has two
 * requests in flight, and a stopped loop ignores an answer that arrives late.
 */
export function startLoop<T>(options: LoopOptions<T>): Loop {
	const now = options.now ?? Date.now;
	const isVisible = options.isVisible ?? (() => document.visibilityState !== 'hidden');
	const setTimer =
		options.setTimer ?? ((callback, delay) => setTimeout(callback, delay) as unknown as number);
	const clearTimer = options.clearTimer ?? ((handle) => clearTimeout(handle));

	let state = initialLoopState<T>();
	let timer: number | null = null;
	let isStopped = false;
	let isInFlight = false;
	let refreshQueued = false;

	function announce(next: LoopState<T>): void {
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
			schedule(options.nextDelay(state));
			return;
		}

		isInFlight = true;
		announce(reduceLoop(state, { type: 'requested' }));
		const result = await options.request();
		isInFlight = false;
		if (isStopped) return;

		announce(
			result.ok
				? reduceLoop(state, { type: 'succeeded', value: result.value, at: now() })
				: reduceLoop(state, { type: 'failed', error: result.error })
		);
		if (refreshQueued) {
			refreshQueued = false;
			schedule(0);
			return;
		}
		schedule(options.nextDelay(state));
	}

	function onVisibilityChange(): void {
		if (!isVisible()) return;
		stopTimer();
		void poll();
	}

	document.addEventListener('visibilitychange', onVisibilityChange);
	options.onChange(state);
	void poll();

	return {
		stop: () => {
			isStopped = true;
			stopTimer();
			document.removeEventListener('visibilitychange', onVisibilityChange);
		},
		refresh: () => {
			if (isStopped) return;
			if (isInFlight) {
				refreshQueued = true;
				return;
			}
			stopTimer();
			void poll();
		}
	};
}

export type PollOptions = {
	/** The request itself; injectable so the loop can be tested without a server. */
	fetchStatus: () => Promise<ParseOutcome<Status>>;
	onChange: (state: PollState) => void;
	now?: () => number;
	isVisible?: () => boolean;
	setTimer?: (callback: () => void, delay: number) => number;
	clearTimer?: (handle: number) => void;
};

/**
 * The launcher's loop: /api/status every 15s while visible, 60s after a failure, and again as soon
 * as the page comes back to the foreground.
 */
export function startPolling(options: PollOptions): () => void {
	const loop = startLoop<Status>({
		request: options.fetchStatus,
		// A failure moves to the slower interval; a success moves back.
		nextDelay: (state) => (state.error === null ? POLL_INTERVAL_MS : RETRY_INTERVAL_MS),
		onChange: (state) => options.onChange(toPollState(state)),
		now: options.now,
		isVisible: options.isVisible,
		setTimer: options.setTimer,
		clearTimer: options.clearTimer
	});
	return loop.stop;
}

/**
 * Adapts the generic loop's state to the launcher's shape. A failure moves to the slower interval,
 * which is the one thing the launcher's state machine adds to the shared one.
 */
function toPollState(state: LoopState<Status>): PollState {
	return {
		status: state.value,
		lastGoodAt: state.lastGoodAt,
		error: state.error,
		isLoading: state.isLoading,
		retryDelay: state.error === null ? POLL_INTERVAL_MS : RETRY_INTERVAL_MS
	};
}
