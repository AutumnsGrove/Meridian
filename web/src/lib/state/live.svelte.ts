import { fetchStatus } from '#lib/api/client';
import { initialPollState, startPolling, type PollState } from './poller';

/**
 * The status a page shows, plus the clock its ages are measured against, fed by the poll loop.
 * `start` must be called from the component's `onMount`: its return value stops the loop again.
 */
export function createLiveStatus() {
	let poll = $state<PollState>(initialPollState());
	let now = $state(Date.now());

	function start(): () => void {
		return startPolling({
			fetchStatus,
			onChange: (next) => {
				poll = next;
				now = Date.now();
			}
		});
	}

	return {
		get poll(): PollState {
			return poll;
		},
		get now(): number {
			return now;
		},
		start
	};
}
