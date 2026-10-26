import { afterEach, describe, expect, test, vi } from 'vitest';
import { fetchStatus } from './client';

const body = {
	generated_at: '2026-10-25T12:00:00Z',
	config: { ok: true, loaded_at: '2026-10-25T11:00:00Z', error: '' },
	summary: { up: 1, total: 1 },
	search: { buttons: [] },
	control: { available: false, reason: '' },
	host: null,
	sections: [
		{
			id: 'media',
			title: 'Media',
			services: [
				{
					id: 'jellyfin',
					name: 'Jellyfin',
					description: '',
					icon: { lucide: 'play' },
					url: 'https://films.example.org',
					status: 'ok',
					reason: '',
					checked_at: '2026-10-25T11:59:57Z',
					vitals: [],
					control: null
				}
			]
		}
	]
};

function stubFetch(implementation: () => Promise<Response>): ReturnType<typeof vi.fn> {
	const fake = vi.fn(implementation);
	vi.stubGlobal('fetch', fake);
	return fake;
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('fetchStatus', () => {
	test('asks /api/status for JSON, same-origin and uncached', async () => {
		const fetch = stubFetch(async () => new Response(JSON.stringify(body), { status: 200 }));

		const result = await fetchStatus();

		expect(result.ok).toBe(true);
		expect(fetch).toHaveBeenCalledTimes(1);
		const [url, init] = fetch.mock.calls[0] as [string, RequestInit];
		expect(url).toBe('/api/status');
		expect(init).toMatchObject({ cache: 'no-store', credentials: 'same-origin' });
	});

	test('refuses a non-2xx response without reading its body', async () => {
		stubFetch(async () => new Response('nope', { status: 503 }));

		const result = await fetchStatus();

		expect(result).toEqual({ ok: false, error: 'status 503' });
	});

	test('refuses a body that is not the contract', async () => {
		stubFetch(async () => new Response(JSON.stringify({ hello: 'world' }), { status: 200 }));

		const result = await fetchStatus();

		expect(result.ok).toBe(false);
	});

	test('refuses a body that is not JSON', async () => {
		stubFetch(async () => new Response('<html></html>', { status: 200 }));

		const result = await fetchStatus();

		expect(result.ok).toBe(false);
	});

	test('turns a network failure into a refused result instead of throwing', async () => {
		stubFetch(() => Promise.reject(new TypeError('Failed to fetch')));

		const result = await fetchStatus();

		expect(result).toEqual({ ok: false, error: 'Failed to fetch' });
	});
});
