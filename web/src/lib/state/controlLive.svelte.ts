import { fetchControlStatus } from '#lib/api/client';
import type { ControlStatus } from '#lib/api/control';
import { controlDelay } from '#lib/state/control';
import { initialLoopState, startLoop, type LoopState } from '#lib/state/poller';

/**
 * The control view a page shows, plus the clock its countdowns are measured against. `start` must be
 * called from the component's `onMount`; its return value stops the loop again.
 */
export function createLiveControl() {
	let loop = $state<LoopState<ControlStatus>>(initialLoopState<ControlStatus>());
	let now = $state(Date.now());
	let handle: { stop: () => void; refresh: () => void } | null = null;

	return {
		get state(): LoopState<ControlStatus> {
			return loop;
		},
		get now(): number {
			return now;
		},
		start(): () => void {
			handle = startLoop<ControlStatus>({
				request: fetchControlStatus,
				// A queued action is watched once a second; an idle page is cheap at ten.
				nextDelay: (state) => controlDelay(state.value?.pending.length ?? 0),
				onChange: (state) => {
					loop = state;
					now = Date.now();
				}
			});
			return () => handle?.stop();
		},
		/** Ask again now, e.g. the moment a tap is queued. */
		refresh(): void {
			handle?.refresh();
		}
	};
}
