import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { loadStatus } from '../../../test/golden';
import { parseControlStatus } from '#lib/api/control';
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

	test('no button on the page is enabled without an executor', async () => {
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

// The control view the container sends, with the parts a test cares about replaced. The real file is
// the Go golden the parser is tested against.
const controlView = (() => {
	const raw: unknown = JSON.parse(
		readFileSync(resolve(process.cwd(), '../testdata/control/status.golden.json'), 'utf8')
	);
	const result = parseControlStatus(raw);
	if (!result.ok) throw new Error(result.error);
	return result.value;
})();

type PageOptions = {
	available: boolean;
	executor: 'ok' | 'missing' | 'unknown';
	pending?: typeof controlView.pending;
	autorestore?: typeof controlView.autorestore;
	results?: typeof controlView.results;
};

function stubPage(options: PageOptions): { posts: unknown[] } {
	const posts: unknown[] = [];
	const statusDocument = {
		...loadStatus('status.all-ok.golden.json'),
		control: {
			available: options.available,
			reason: options.available ? '' : 'control plane not installed'
		}
	};
	const view = {
		...controlView,
		executor: { state: options.executor, since: null },
		pending: options.pending ?? [],
		autorestore: options.autorestore ?? [],
		results: options.results ?? []
	};

	vi.stubGlobal(
		'fetch',
		vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = String(input);
			if (url === '/api/status')
				return new Response(JSON.stringify(statusDocument), { status: 200 });
			if (url === '/api/control/status') return new Response(JSON.stringify(view), { status: 200 });
			if (url === '/api/control') {
				posts.push(JSON.parse(String(init?.body)));
				return new Response(JSON.stringify({ id: '0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e77' }), {
					status: 202
				});
			}
			return new Response('no', { status: 404 });
		})
	);
	return { posts };
}

// The page lists one row per controllable service, so a button name alone is ambiguous: scope to the
// row under test.
function metricsRow(): HTMLElement {
	const heading = screen.getByRole('heading', { level: 2, name: 'Metrics' });
	const row = heading.closest('li');
	if (row === null) throw new Error('the Metrics row is not a list item');
	return row;
}

describe('the live control page', () => {
	test('offers a working button on every row when the control plane is installed', async () => {
		stubPage({ available: true, executor: 'ok' });
		render(Page);

		await screen.findByRole('heading', { level: 2, name: 'Metrics' });
		const restart = within(metricsRow()).getByRole('button', {
			name: 'restart'
		}) as HTMLButtonElement;

		expect(restart.disabled).toBe(false);
		expect((screen.getByRole('button', { name: /reboot/ }) as HTMLButtonElement).disabled).toBe(
			false
		);
	});

	test('a restart is one tap, and the row says it is queued', async () => {
		const { posts } = stubPage({ available: true, executor: 'ok' });
		render(Page);

		await screen.findByRole('heading', { level: 2, name: 'Metrics' });
		fireEvent.click(within(metricsRow()).getByRole('button', { name: 'restart' }));

		expect(posts).toEqual([{ action: 'restart', target: 'metrics', ack: '' }]);
		await waitFor(() => expect(screen.getByText(/restarting/)).toBeTruthy());
	});

	test('a DNS stop arms first and says what breaks before anything is sent', async () => {
		const { posts } = stubPage({ available: true, executor: 'ok' });
		render(Page);

		await screen.findByRole('heading', { level: 2, name: 'Metrics' });
		fireEvent.click(within(metricsRow()).getByRole('button', { name: 'stop' }));

		expect(posts).toEqual([]);
		expect(await screen.findByText(/breaks DNS for the whole tailnet/)).toBeTruthy();
		fireEvent.click(await screen.findByRole('button', { name: 'stop anyway' }));

		expect(posts).toEqual([{ action: 'stop', target: 'metrics', ack: 'dns-stop-confirmed' }]);
	});

	test('cancel disarms without sending anything', async () => {
		const { posts } = stubPage({ available: true, executor: 'ok' });
		render(Page);

		await screen.findByRole('heading', { level: 2, name: 'Metrics' });
		fireEvent.click(within(metricsRow()).getByRole('button', { name: 'stop' }));
		await screen.findByText(/breaks DNS for the whole tailnet/);
		fireEvent.click(screen.getByRole('button', { name: 'cancel' }));

		expect(posts).toEqual([]);
		await waitFor(() => expect(screen.queryByText(/breaks DNS for the whole tailnet/)).toBeNull());
	});

	test('says why nothing is moving when the executor is gone, and marks the queued request', async () => {
		stubPage({
			available: true,
			executor: 'missing',
			pending: [
				{
					id: '0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e66',
					action: 'restart',
					target: 'metrics',
					requested_at: '2026-10-25T11:59:40Z',
					age_s: 20,
					unclaimed: true
				}
			]
		});
		render(Page);

		expect(
			await screen.findByText('No executor. Actions are queued but not being picked up.')
		).toBeTruthy();
		expect(screen.getByText('restart pending (unclaimed)')).toBeTruthy();
	});
});
