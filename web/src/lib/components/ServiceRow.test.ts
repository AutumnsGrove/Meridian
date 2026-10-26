import { render, screen } from '@testing-library/svelte';
import { describe, expect, test } from 'vitest';
import { loadStatus, serviceNamed } from '../../../test/golden';
import ServiceRow from './ServiceRow.svelte';

const status = loadStatus('status.all-ok.golden.json');
const control = status.control;
const metrics = serviceNamed(status, 'Metrics');

function describedText(button: HTMLElement): string {
	const ids = button.getAttribute('aria-describedby')?.split(' ') ?? [];
	return ids
		.map((id) => document.getElementById(id)?.textContent ?? '')
		.join(' ')
		.trim();
}

describe('ServiceRow', () => {
	test('shows the name, the state as a word, and one button per action', () => {
		render(ServiceRow, { service: metrics, control });

		expect(screen.getByRole('heading', { level: 3, name: 'Metrics' })).toBeTruthy();
		expect(screen.getByText('healthy')).toBeTruthy();
		expect(screen.getAllByRole('button').map((button) => button.textContent?.trim())).toEqual([
			'start',
			'restart',
			'stop'
		]);
	});

	test('every action is disabled in Phase 1 and describes why', () => {
		render(ServiceRow, { service: metrics, control });

		for (const button of screen.getAllByRole('button') as HTMLButtonElement[]) {
			expect(button.disabled).toBe(true);
			expect(describedText(button)).toContain('control plane not installed');
		}
	});

	test('no button is a form submit, so pressing one can never post anything', () => {
		const { container } = render(ServiceRow, { service: metrics, control });

		expect(container.querySelector('form')).toBeNull();
		for (const button of screen.getAllByRole('button')) {
			expect(button.getAttribute('type')).toBe('button');
		}
	});

	test('an update the allowlist refuses shows its own reason, on the button and to a screen reader', () => {
		const service = {
			...metrics,
			control: metrics.control && {
				...metrics.control,
				actions: ['restart', 'update'],
				update_disabled_reason: 'no scripted updater'
			}
		};

		render(ServiceRow, { service, control });

		const update = screen.getByRole('button', { name: /update/ });

		expect(update.textContent).toContain('no scripted updater');
		expect(describedText(update)).toContain('no scripted updater');
		expect(update.hasAttribute('disabled')).toBe(true);
	});

	test('takes its id from the service, so a link can land on it', () => {
		const { container } = render(ServiceRow, { service: metrics, control });

		expect(container.querySelector('li')?.id).toBe('metrics');
	});

	test('marks itself when the URL points at it', () => {
		const { container } = render(ServiceRow, { service: metrics, control, isHighlighted: true });

		expect(container.querySelector('li')?.className).toContain('highlighted');
	});

	test('is not marked when it is not the target', () => {
		const { container } = render(ServiceRow, { service: metrics, control, isHighlighted: false });

		expect(container.querySelector('li')?.className).not.toContain('highlighted');
	});
});
