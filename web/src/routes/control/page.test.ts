import { render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { loadStatus } from '../../../test/golden';
import Page from './+page.svelte';

const status = loadStatus('status.all-ok.golden.json');

function stubFetch(): void {
	vi.stubGlobal(
		'fetch',
		vi.fn(async () => new Response(JSON.stringify(status), { status: 200 }))
	);
}

beforeEach(() => {
	stubFetch();
	window.location.hash = '';
	Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
	window.location.hash = '';
});

describe('the control placeholder', () => {
	test('says the control plane is not installed and lists every controllable service', async () => {
		render(Page);

		await waitFor(() =>
			expect(screen.getByText('Control plane not installed on this box')).toBeTruthy()
		);
		expect(screen.getByRole('heading', { level: 1, name: 'Control' })).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: 'Metrics' })).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: 'Gitea' })).toBeTruthy();
		// Jellyfin and the backup daemon have no control block, so they are not rows.
		expect(screen.queryByRole('heading', { level: 2, name: 'Jellyfin' })).toBeNull();
	});

	test('no button on the page is enabled in Phase 1', async () => {
		render(Page);

		await waitFor(() => expect(screen.getAllByRole('button').length).toBeGreaterThan(0));

		for (const button of screen.getAllByRole('button') as HTMLButtonElement[]) {
			expect(button.disabled).toBe(true);
		}
	});

	test('no button is a form submit, so the page cannot post anything', async () => {
		const { container } = render(Page);

		await waitFor(() => expect(screen.getAllByRole('button').length).toBeGreaterThan(0));

		expect(container.querySelector('form')).toBeNull();
		for (const button of screen.getAllByRole('button')) {
			expect(button.getAttribute('type')).toBe('button');
		}
	});

	test('shows the emergency reboot and disables it with the reason', async () => {
		render(Page);

		await waitFor(() =>
			expect(screen.getByRole('heading', { level: 2, name: 'Host' })).toBeTruthy()
		);
		expect((screen.getByRole('button', { name: /reboot/ }) as HTMLButtonElement).disabled).toBe(
			true
		);
	});

	test('a #id in the URL marks that row and scrolls to it once the data arrives', async () => {
		window.location.hash = '#gitea';

		const { container } = render(Page);

		await waitFor(() => expect(container.querySelector('#gitea')).not.toBeNull());
		const row = container.querySelector('#gitea');

		expect(row?.className).toContain('highlighted');
		expect(Element.prototype.scrollIntoView).toHaveBeenCalled();
	});

	test('a #id that is not a row marks nothing and scrolls nowhere', async () => {
		window.location.hash = '#<script>';

		const { container } = render(Page);

		await waitFor(() => expect(screen.getByRole('heading', { name: 'Control' })).toBeTruthy());

		expect(container.querySelector('.highlighted')).toBeNull();
		expect(Element.prototype.scrollIntoView).not.toHaveBeenCalled();
	});

	test('links back to the launcher', async () => {
		render(Page);

		await waitFor(() => expect(screen.getByRole('link', { name: /Meridian/ })).toBeTruthy());

		expect(screen.getByRole('link', { name: /Meridian/ }).getAttribute('href')).toBe('/');
	});
});
