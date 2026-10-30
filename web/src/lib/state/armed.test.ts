import fc from 'fast-check';
import { describe, expect, test } from 'vitest';
import {
	ARM_WINDOW_MS,
	initialArmState,
	needsArming,
	reduceArm,
	type ArmEvent,
	type ArmState
} from './armed';

/** A stream of events over one page's lifetime, each carrying its own clock. */
const eventArb: fc.Arbitrary<ArmEvent> = fc.oneof(
	fc.record({
		type: fc.constant('tap' as const),
		action: fc.constantFrom('start', 'restart', 'update', 'stop', 'reboot'),
		target: fc.constantFrom('app', 'resolver', 'host'),
		now: fc.integer({ min: 0, max: 60_000 })
	}),
	fc.record({ type: fc.constant('expire' as const), now: fc.integer({ min: 0, max: 60_000 }) }),
	fc.record({ type: fc.constant('sent' as const), id: fc.uuid() }),
	fc.record({
		type: fc.constant('result' as const),
		id: fc.uuid(),
		ok: fc.boolean(),
		sentence: fc.string()
	}),
	fc.record({ type: fc.constant('refused' as const), sentence: fc.string() }),
	fc.constant<ArmEvent>({ type: 'cancel' })
);

function tap(action: string, now = 0, target = 'app'): ArmEvent {
	return { type: 'tap', action, target, now };
}

describe('needsArming', () => {
	test('is true for the two actions whose consequences outlast the press', () => {
		expect(needsArming('stop')).toBe(true);
		expect(needsArming('reboot')).toBe(true);
	});

	test('is false for the single-tap actions', () => {
		for (const action of ['start', 'restart', 'update']) {
			expect(needsArming(action)).toBe(false);
		}
	});
});

describe('reduceArm', () => {
	test('a single-tap action sends straight from idle', () => {
		const { state, effect } = reduceArm(initialArmState(), tap('restart'));

		expect(effect).toEqual({ type: 'send', action: 'restart' });
		expect(state).toEqual({ phase: 'sending', action: 'restart' });
	});

	test('a destructive action arms first and sends nothing', () => {
		const { state, effect } = reduceArm(initialArmState(), tap('stop'));

		expect(effect).toEqual({ type: 'none' });
		expect(state).toEqual({
			phase: 'armed',
			action: 'stop',
			target: 'app',
			expiresAt: ARM_WINDOW_MS
		});
	});

	test("a tap on another row does not spend the first row's arming", () => {
		const armed = reduceArm(initialArmState(), tap('stop', 0, 'app')).state;
		const { state, effect } = reduceArm(armed, tap('stop', 1_000, 'resolver'));

		// The second row was never armed, so this tap arms it and sends nothing.
		expect(effect).toEqual({ type: 'none' });
		expect(state).toEqual({
			phase: 'armed',
			action: 'stop',
			target: 'resolver',
			expiresAt: 1_000 + ARM_WINDOW_MS
		});
	});

	test('the second tap inside the window sends', () => {
		const armed = reduceArm(initialArmState(), tap('stop')).state;
		const { state, effect } = reduceArm(armed, tap('stop', ARM_WINDOW_MS - 1));

		expect(effect).toEqual({ type: 'send', action: 'stop' });
		expect(state).toEqual({ phase: 'sending', action: 'stop' });
	});

	test('a tap after the window re-arms instead of sending', () => {
		const armed = reduceArm(initialArmState(), tap('stop')).state;
		const { state, effect } = reduceArm(armed, tap('stop', ARM_WINDOW_MS));

		expect(effect).toEqual({ type: 'none' });
		expect(state).toEqual({
			phase: 'armed',
			action: 'stop',
			target: 'app',
			expiresAt: ARM_WINDOW_MS * 2
		});
	});

	test('expiring returns an armed button to rest, and a late expiry is ignored', () => {
		const armed = reduceArm(initialArmState(), tap('stop')).state;

		expect(reduceArm(armed, { type: 'expire', now: ARM_WINDOW_MS - 1 }).state).toEqual(armed);
		expect(reduceArm(armed, { type: 'expire', now: ARM_WINDOW_MS }).state).toEqual({
			phase: 'idle'
		});
	});

	test('cancel resets an armed button and does nothing else', () => {
		const armed = reduceArm(initialArmState(), tap('reboot')).state;

		expect(reduceArm(armed, { type: 'cancel' }).state).toEqual({ phase: 'idle' });
		expect(reduceArm(initialArmState(), { type: 'cancel' }).state).toEqual({ phase: 'idle' });
	});

	test('a sent request is pending by id, then done when its result arrives', () => {
		const sending = reduceArm(initialArmState(), tap('restart')).state;
		const pending = reduceArm(sending, { type: 'sent', id: 'one' }).state;

		expect(pending).toEqual({ phase: 'pending', action: 'restart', id: 'one' });
		expect(reduceArm(pending, { type: 'result', id: 'one', ok: true, sentence: '' }).state).toEqual(
			{ phase: 'done', action: 'restart', id: 'one', ok: true, sentence: '' }
		);
	});

	test('a result for another request leaves the row alone', () => {
		const pending: ArmState = { phase: 'pending', action: 'restart', id: 'one' };

		expect(
			reduceArm(pending, { type: 'result', id: 'two', ok: false, sentence: '' }).state
		).toEqual(pending);
	});

	test('a refused request lands on done with the reason and no id', () => {
		const sending = reduceArm(initialArmState(), tap('restart')).state;
		const { state } = reduceArm(sending, { type: 'refused', sentence: 'The queue is full.' });

		expect(state).toEqual({
			phase: 'done',
			action: 'restart',
			id: null,
			ok: false,
			sentence: 'The queue is full.'
		});
	});

	test('a tap while a request is in flight is ignored, so one action runs at a time', () => {
		for (const state of [
			{ phase: 'sending', action: 'restart' } as ArmState,
			{ phase: 'pending', action: 'restart', id: 'one' } as ArmState
		]) {
			const outcome = reduceArm(state, tap('stop'));

			expect(outcome.state).toEqual(state);
			expect(outcome.effect).toEqual({ type: 'none' });
		}
	});

	test('a result can arrive before the POST answer and still completes the row', () => {
		const sending = reduceArm(initialArmState(), tap('restart')).state;
		const { state } = reduceArm(sending, { type: 'result', id: 'one', ok: true, sentence: '' });

		expect(state).toEqual({ phase: 'done', action: 'restart', id: 'one', ok: true, sentence: '' });
	});
});

describe('the armed rule, over arbitrary event streams', () => {
	test('a destructive send only happens through an armed state inside its window', () => {
		fc.assert(
			fc.property(fc.array(eventArb, { maxLength: 40 }), (events) => {
				let state = initialArmState();

				for (const event of events) {
					const before = state;
					const outcome = reduceArm(state, event);
					if (outcome.effect.type === 'send' && needsArming(outcome.effect.action)) {
						if (before.phase !== 'armed' || before.action !== outcome.effect.action) return false;
						// The tap that sends has to be inside the window it armed, on the row it armed.
						if (event.type !== 'tap' || event.now >= before.expiresAt) return false;
						if (event.target !== before.target) return false;
					}
					state = outcome.state;
				}
				return true;
			}),
			{ numRuns: 500 }
		);
	});

	test('one action is in flight at most once: a send never follows a send', () => {
		fc.assert(
			fc.property(fc.array(eventArb, { maxLength: 40 }), (events) => {
				let state = initialArmState();
				let previousSent = false;

				for (const event of events) {
					const outcome = reduceArm(state, event);
					const sends = outcome.effect.type === 'send';
					if (sends && previousSent) return false;
					previousSent = sends;
					state = outcome.state;
				}
				return true;
			}),
			{ numRuns: 500 }
		);
	});

	test('the state is always one of the five phases', () => {
		fc.assert(
			fc.property(fc.array(eventArb, { maxLength: 40 }), (events) => {
				let state = initialArmState();

				for (const event of events) {
					state = reduceArm(state, event).state;
					if (!['idle', 'armed', 'sending', 'pending', 'done'].includes(state.phase)) return false;
				}
				return true;
			}),
			{ numRuns: 500 }
		);
	});
});
