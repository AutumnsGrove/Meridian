import { render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, test, vi } from 'vitest';
import { loadStatus } from '../../test/golden';
import Page from './+page.svelte';

const status = loadStatus('status.all-ok.golden.json');

function stubFetch(): ReturnType<typeof vi.fn> {
	const fake = vi.fn(async () => new Response(JSON.stringify(status), { status: 200 }));
	vi.stubGlobal('fetch', fake);
	return fake;
}

afterEach(() => {
	vi.useRealTimers();
	vi.unstubAllGlobals();
});

describe('the launcher route', () => {
	test('shows the summary line, the search buttons and every section from the API', async () => {
		const fetch = stubFetch();

		render(Page);

		await waitFor(() => expect(screen.getByText('4/4 up')).toBeTruthy());
		expect(fetch).toHaveBeenCalledWith('/api/status', expect.anything());
		expect(screen.getByRole('searchbox')).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Search the web' })).toBeTruthy();
		expect(screen.getByRole('heading', { name: 'Media' })).toBeTruthy();
		expect(screen.getByRole('heading', { name: 'Development' })).toBeTruthy();
	});

	test('offers one way into the control page and nothing that changes state', async () => {
		stubFetch();

		render(Page);

		await waitFor(() => expect(screen.getByText('4/4 up')).toBeTruthy());

		const control = screen.getByRole('link', { name: /Control panel/ });
		expect(control.getAttribute('href')).toBe('/control');
		// The launcher itself never acts: the only buttons on the page are the search engines.
		expect(screen.getAllByRole('button').map((button) => button.textContent)).toEqual([
			'Search the web'
		]);
	});

	test('keeps the last good data and says so when the next request fails', async () => {
		vi.useFakeTimers();
		let answer = new Response(JSON.stringify(status), { status: 200 });
		vi.stubGlobal(
			'fetch',
			vi.fn(async () => answer)
		);

		render(Page);
		await vi.advanceTimersByTimeAsync(0);
		expect(screen.getByText('4/4 up')).toBeTruthy();

		answer = new Response('down', { status: 502 });
		await vi.advanceTimersByTimeAsync(15_000);

		expect(screen.getByRole('status').textContent).toContain("Can't reach Meridian");
		expect(screen.getByText('4/4 up')).toBeTruthy();
	});

	test('says the config was rejected while still serving the old one', async () => {
		const rejected = loadStatus('status.config-rejected.golden.json');
		vi.stubGlobal(
			'fetch',
			vi.fn(async () => new Response(JSON.stringify(rejected), { status: 200 }))
		);

		render(Page);

		await waitFor(() => expect(screen.getByRole('status')).toBeTruthy());
		expect(screen.getByRole('status').textContent).toContain('The last config change was rejected');
	});
});
