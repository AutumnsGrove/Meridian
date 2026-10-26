import { describe, expect, test } from 'vitest';
import { loadStatus, serviceNamed } from '../../../test/golden';
import type { ControlAvailability } from '#lib/api/types';
import { controlNote, controlReason, controlRows, rowActions, targetID } from './control';

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
	test('says the control plane is not installed in Phase 1', () => {
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
