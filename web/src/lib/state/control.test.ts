import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, test } from 'vitest';
import { loadStatus, serviceNamed } from '../../../test/golden';
import { parseControlStatus } from '#lib/api/control';
import type { ControlAvailability } from '#lib/api/types';
import {
	CONTROL_FOLLOW_UP_MS,
	CONTROL_IDLE_MS,
	busyReason,
	controlDelay,
	controlNote,
	controlReason,
	controlRows,
	countdownLabel,
	historyRows,
	isRebooting,
	nameForTarget,
	pendingFor,
	ackFor,
	armedWarning,
	progressLabel,
	remainingSeconds,
	resultLine,
	resultFor,
	rowActions,
	targetID
} from './control';

const status = loadStatus('status.all-ok.golden.json');
const unavailable: ControlAvailability = status.control;
const available: ControlAvailability = { available: true, reason: '' };

describe('controlRows', () => {
	test('lists only the services with a control block, in config order', () => {
		expect(controlRows(status).map((service) => service.name)).toEqual(['Metrics', 'Gitea']);
	});

	test('a config with no controllable service lists nothing', () => {
		const readOnly = {
			...status,
			sections: status.sections.map((section) => ({
				...section,
				services: section.services.map((service) => ({ ...service, control: null }))
			}))
		};

		expect(controlRows(readOnly)).toEqual([]);
	});
});

describe('rowActions', () => {
	test('offers one button per action the allowlist names, in the order it names them', () => {
		const metrics = serviceNamed(status, 'Metrics');

		expect(rowActions(metrics, unavailable).map((row) => row.label)).toEqual([
			'start',
			'restart',
			'stop'
		]);
	});

	test('every button carries the reason the control plane cannot run, so none is silently dead', () => {
		const gitea = serviceNamed(status, 'Gitea');

		for (const row of rowActions(gitea, unavailable)) {
			expect(row.reason).toBe('control plane not installed');
		}
	});

	test('has no reason to show once the control plane is available', () => {
		const gitea = serviceNamed(status, 'Gitea');

		for (const row of rowActions(gitea, available)) {
			expect(row.reason).toBe('');
			expect(row.detail).toBe('');
		}
	});

	test('an update the allowlist cannot run keeps its own reason on the button', () => {
		const gitea = serviceNamed(status, 'Gitea');
		const service = {
			...gitea,
			control: gitea.control && {
				...gitea.control,
				actions: [...gitea.control.actions, 'update'],
				update_disabled_reason: 'no scripted updater'
			}
		};

		const update = rowActions(service, unavailable).find((row) => row.action === 'update');

		expect(update?.detail).toBe('no scripted updater');
	});

	test('only update has a second reason; the others are only refused by the phase', () => {
		const gitea = serviceNamed(status, 'Gitea');
		const service = {
			...gitea,
			control: gitea.control && {
				...gitea.control,
				actions: [...gitea.control.actions, 'update'],
				update_disabled_reason: 'no scripted updater'
			}
		};

		const details = rowActions(service, unavailable)
			.filter((row) => row.detail !== '')
			.map((row) => row.action);

		expect(details).toEqual(['update']);
	});
});

describe('controlReason', () => {
	test('is the server sentence when it sent one', () => {
		expect(controlReason(unavailable)).toBe('control plane not installed');
	});

	test('falls back to one sentence when the reason is empty', () => {
		expect(controlReason({ available: false, reason: '' })).toBe('control plane not installed');
	});
});

describe('controlNote', () => {
	test('says the control plane is not installed when the mounts are absent', () => {
		expect(controlNote(unavailable)).toBe('Control plane not installed on this box');
	});

	test('says nothing when the control plane exists', () => {
		expect(controlNote(available)).toBeNull();
	});
});

describe('targetID', () => {
	test.each([
		{ hash: '#gitea', want: 'gitea' },
		{ hash: 'gitea', want: 'gitea' },
		{ hash: '#a-1', want: 'a-1' },
		{ hash: '', want: null },
		{ hash: '#', want: null },
		{ hash: '#Gitea', want: null },
		{ hash: '#../etc/passwd', want: null },
		{ hash: '#a b', want: null },
		{ hash: '#<script>', want: null },
		{ hash: '#' + 'a'.repeat(33), want: null }
	])('reads $hash as $want', ({ hash, want }) => {
		expect(targetID(hash)).toBe(want);
	});
});

// The control view the container sends (testdata/control/status.golden.json), parsed once through the
// same validator the page uses.
const control = (() => {
	const raw: unknown = JSON.parse(
		readFileSync(resolve(process.cwd(), '../testdata/control/status.golden.json'), 'utf8')
	);
	const result = parseControlStatus(raw);
	if (!result.ok) throw new Error(result.error);
	return result.value;
})();

describe('controlDelay', () => {
	test('follows a queued action once a second and idles at ten', () => {
		expect(controlDelay(1)).toBe(CONTROL_FOLLOW_UP_MS);
		expect(controlDelay(0)).toBe(CONTROL_IDLE_MS);
	});
});

describe('pendingFor and resultFor', () => {
	test('find a target in the view the container sent', () => {
		expect(pendingFor(control, 'one')?.action).toBe('stop');
		expect(pendingFor(control, 'two')).toBeNull();
		expect(resultFor(control, 'one')?.action).toBe('update');
	});
});

describe('busyReason', () => {
	test('names the other target that is in the way', () => {
		expect(busyReason(control, 'two')).toBe('Waiting for one to finish stop.');
	});

	test('says nothing when the only queued request is this row\u2019s own', () => {
		expect(busyReason(control, 'one')).toBe('');
	});

	test('names an action that is still running even when the queue is empty', () => {
		const running = {
			...control,
			pending: [],
			results: [{ ...control.results[0], in_progress: true }]
		};

		expect(busyReason(running, 'two')).toBe('Waiting for an action that is still running.');
	});
});

describe('the auto-restore countdown', () => {
	const restore = control.autorestore[0];

	test('counts down from the browser clock, and never below zero', () => {
		const now = Date.parse(restore.restore_at) - 90_000;

		expect(remainingSeconds(restore, now)).toBe(90);
		expect(remainingSeconds(restore, Date.parse(restore.restore_at) + 60_000)).toBe(0);
	});

	test('renders as a clock a person can read', () => {
		expect(countdownLabel(599)).toBe('9:59');
		expect(countdownLabel(0)).toBe('0:00');
		expect(countdownLabel(-5)).toBe('0:00');
	});
});

describe('historyRows', () => {
	test('names the action, its target, its duration and the executor sentence', () => {
		const rows = historyRows(control);

		expect(rows).toHaveLength(1);
		expect(rows[0].text).toBe('stop one');
		expect(rows[0].duration).toBe('2.0s');
		expect(rows[0].ok).toBe(true);
		expect(rows[0].time).toMatch(/^\d{2}:\d{2}$/);
	});

	test('a failed row explains itself with the sentence the container supplied', () => {
		const failed = {
			...control,
			audit: [
				{
					...control.audit[0],
					ok: false,
					code: 'exec_failed',
					sentence: 'The command failed.',
					output: 'docker: not found'
				}
			]
		};

		expect(historyRows(failed)[0].detail).toBe('The command failed.');
	});
});

describe('the words a row uses', () => {
	test('a button in flight says what is happening, in English', () => {
		expect(progressLabel('stop')).toBe('stopping…');
		expect(progressLabel('restart')).toBe('restarting…');
		expect(progressLabel('unknown')).toBe('unknown…');
	});

	test('an armed stop on a DNS target says what breaks and when it comes back', () => {
		const resolver = {
			...serviceNamed(loadStatus('status.all-ok.golden.json'), 'Metrics'),
			control: { target: 'one', actions: ['stop'], update_disabled_reason: '', dns: true }
		};

		expect(armedWarning(resolver, 'stop')).toContain('breaks DNS for the whole tailnet');
		expect(armedWarning(resolver, 'stop')).toContain('comes back in 10 minutes');
	});

	test('an armed stop elsewhere promises only what is true', () => {
		const service = {
			...serviceNamed(loadStatus('status.all-ok.golden.json'), 'Metrics'),
			control: { target: 'one', actions: ['stop'], update_disabled_reason: '', dns: false }
		};

		expect(armedWarning(service, 'stop')).toBe(
			`Stopping ${service.name} takes it offline until you start it again.`
		);
	});

	test('a finished row reports the duration, and a failure reports the sentence', () => {
		expect(resultLine(control.results[1])).toBe('stopped in 2.0s');
		expect(resultLine(control.results[0])).toBe('That target does not allow this action.');
		expect(resultLine({ ...control.results[0], in_progress: true })).toBe('In progress.');
	});

	test('no sentence in this module uses an em dash or an exclamation mark', () => {
		const service = serviceNamed(loadStatus('status.all-ok.golden.json'), 'Metrics');
		const sentences = [
			armedWarning(service, 'stop'),
			progressLabel('stop'),
			resultLine(control.results[0]),
			countdownLabel(59),
			historyRows(control)[0].text
		];

		for (const sentence of sentences) {
			expect(sentence).not.toMatch(/[!\u2014]/);
		}
	});
});

describe('the acknowledgement a tap carries', () => {
	test('matches the executor table: a reboot, a DNS stop and a plain stop differ', () => {
		expect(ackFor('reboot', false)).toBe('reboot-confirmed');
		expect(ackFor('stop', false)).toBe('stop-confirmed');
		expect(ackFor('stop', true)).toBe('dns-stop-confirmed');
		expect(ackFor('restart', true)).toBe('');
	});
});

describe('isRebooting', () => {
	test('is true while a reboot is in flight and its result has not landed', () => {
		expect(isRebooting({ phase: 'pending', action: 'reboot', id: 'x' }, control)).toBe(true);
		expect(isRebooting({ phase: 'sending', action: 'reboot' }, control)).toBe(true);
	});

	test('is false for anything else, so the reconnect loop never runs for a restart', () => {
		expect(isRebooting({ phase: 'pending', action: 'restart', id: 'x' }, control)).toBe(false);
		expect(isRebooting({ phase: 'armed', action: 'reboot', expiresAt: 1 }, control)).toBe(false);
	});
});

describe('nameForTarget', () => {
	test('finds the row a restore belongs to, and falls back to the slug', () => {
		const service = serviceNamed(loadStatus('status.all-ok.golden.json'), 'Metrics');

		expect(nameForTarget([service], service.control?.target ?? '')).toBe('Metrics');
		expect(nameForTarget([service], 'nobody')).toBe('nobody');
	});
});
