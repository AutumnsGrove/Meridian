/**
 * The two-step button (docs/DESIGN.md "Control panel", decision #16). The executor checks the ack;
 * this is the courtesy that makes a mis-tap cheap, and it is pure so the property can be tested: a
 * stop or a reboot can only be sent from an armed state, inside the five seconds it was armed for.
 */

/** How long an armed button waits for its second tap. */
export const ARM_WINDOW_MS = 5_000;

/** The actions that arm first. Everything else is one tap. */
const DESTRUCTIVE = new Set(['stop', 'reboot']);

export function needsArming(action: string): boolean {
	return DESTRUCTIVE.has(action);
}

export type ArmState =
	| { phase: 'idle' }
	| { phase: 'armed'; action: string; target: string; expiresAt: number }
	| { phase: 'sending'; action: string }
	| { phase: 'pending'; action: string; id: string }
	| { phase: 'done'; action: string; id: string | null; ok: boolean; sentence: string };

export type ArmEvent =
	/** A press on a row's button, at the browser's clock. The target is the row the press belongs to. */
	| { type: 'tap'; action: string; target: string; now: number }
	/** The arming window ran out. */
	| { type: 'expire'; now: number }
	/** The request exists and has an id. */
	| { type: 'sent'; id: string }
	/** The container refused the request; nothing was queued. */
	| { type: 'refused'; sentence: string }
	/** The executor answered, possibly before the POST did. */
	| { type: 'result'; id: string; ok: boolean; sentence: string }
	/** The person changed their mind, or the page is moving on. */
	| { type: 'cancel' };

export type ArmEffect = { type: 'send'; action: string } | { type: 'none' };

export function initialArmState(): ArmState {
	return { phase: 'idle' };
}

/**
 * The whole machine: a destructive action arms for ARM_WINDOW_MS and only the second tap sends, a
 * single-tap action sends at once, and nothing sends while something is in flight.
 */
export function reduceArm(
	state: ArmState,
	event: ArmEvent
): { state: ArmState; effect: ArmEffect } {
	switch (event.type) {
		case 'tap': {
			if (state.phase === 'sending' || state.phase === 'pending') {
				return { state, effect: { type: 'none' } };
			}
			if (!needsArming(event.action)) {
				return { state: { phase: 'sending', action: event.action }, effect: send(event.action) };
			}
			// The second tap belongs to the row that was armed. Another row's button for the same action
			// is a first tap of its own: it arms that row, and the old arming is spent.
			if (
				state.phase === 'armed' &&
				state.action === event.action &&
				state.target === event.target &&
				event.now < state.expiresAt
			) {
				return { state: { phase: 'sending', action: event.action }, effect: send(event.action) };
			}
			return {
				state: {
					phase: 'armed',
					action: event.action,
					target: event.target,
					expiresAt: event.now + ARM_WINDOW_MS
				},
				effect: { type: 'none' }
			};
		}
		case 'expire':
			if (state.phase === 'armed' && event.now >= state.expiresAt) {
				return { state: { phase: 'idle' }, effect: { type: 'none' } };
			}
			return { state, effect: { type: 'none' } };
		case 'sent':
			if (state.phase !== 'sending') return { state, effect: { type: 'none' } };
			return {
				state: { phase: 'pending', action: state.action, id: event.id },
				effect: { type: 'none' }
			};
		case 'refused':
			if (state.phase !== 'sending') return { state, effect: { type: 'none' } };
			return {
				state: {
					phase: 'done',
					action: state.action,
					id: null,
					ok: false,
					sentence: event.sentence
				},
				effect: { type: 'none' }
			};
		case 'result':
			if (state.phase !== 'sending' && state.phase !== 'pending') {
				return { state, effect: { type: 'none' } };
			}
			if (state.phase === 'pending' && state.id !== event.id) {
				return { state, effect: { type: 'none' } };
			}
			return {
				state: {
					phase: 'done',
					action: state.action,
					id: event.id,
					ok: event.ok,
					sentence: event.sentence
				},
				effect: { type: 'none' }
			};
		case 'cancel':
			return state.phase === 'armed' || state.phase === 'done'
				? { state: { phase: 'idle' }, effect: { type: 'none' } }
				: { state, effect: { type: 'none' } };
	}
}

function send(action: string): ArmEffect {
	return { type: 'send', action };
}
