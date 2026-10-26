import { render, screen } from '@testing-library/svelte';
import { describe, expect, test } from 'vitest';
import { loadStatus } from '../../../test/golden';
import RebootCard from './RebootCard.svelte';

const control = loadStatus('status.all-ok.golden.json').control;

describe('RebootCard', () => {
	test('is visible in Phase 1, with the action refused and the reason given', () => {
		render(RebootCard, { reason: 'control plane not installed' });

		expect(screen.getByRole('heading', { level: 2, name: 'Host' })).toBeTruthy();
		const reboot = screen.getByRole('button', { name: /reboot/ }) as HTMLButtonElement;

		expect(reboot.disabled).toBe(true);
		expect(reboot.getAttribute('type')).toBe('button');
		const described = document.getElementById(reboot.getAttribute('aria-describedby') ?? '');
		expect(described?.textContent).toContain('control plane not installed');
	});

	test('says what a reboot costs before anyone presses it', () => {
		render(RebootCard, { reason: 'control plane not installed' });

		expect(screen.getByText(/offline for about 40 seconds/)).toBeTruthy();
	});

	test('is driven by the same control state as the rows, not by a page of its own', () => {
		render(RebootCard, { reason: control.reason });

		expect(screen.getByRole('button', { name: /reboot/ })).toBeTruthy();
	});
});
