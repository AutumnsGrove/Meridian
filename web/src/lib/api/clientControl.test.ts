import { afterEach, describe, expect, test, vi } from 'vitest';
import { fetchControlStatus, postControl } from './client';

function respondWith(status: number, body: unknown): void {
	vi.stubGlobal(
		'fetch',
		vi.fn(async () => new Response(JSON.stringify(body), { status }))
	);
}

afterEach(() => {
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
});

describe('postControl', () => {
	test('sends only what the config declares and returns the queued id', async () => {
		respondWith(202, { id: '0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44' });

		const outcome = await postControl({ action: 'restart', target: 'gitea', ack: '' });

		expect(outcome).toEqual({ ok: true, id: '0192f0c4-6c1a-7f3b-9a10-2b6f7c1d8e44' });
		const [url, init] = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0] as [
			string,
			RequestInit
		];
		expect(url).toBe('/api/control');
		expect(init.method).toBe('POST');
		expect(JSON.parse(String(init.body))).toEqual({ action: 'restart', target: 'gitea', ack: '' });
	});

	test('turns a refusal into the container sentence, not a code of its own', async () => {
		respondWith(429, { code: 'queue_full', sentence: 'The queue is full. Wait a moment.' });

		const outcome = await postControl({ action: 'start', target: 'gitea', ack: '' });

		expect(outcome).toEqual({
			ok: false,
			code: 'queue_full',
			sentence: 'The queue is full. Wait a moment.'
		});
	});

	test('a body that is not the error shape still yields a sentence the page can show', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(async () => new Response('<html>bad gateway</html>', { status: 502 }))
		);

		const outcome = await postControl({ action: 'start', target: 'gitea', ack: '' });

		expect(outcome.ok).toBe(false);
		if (!outcome.ok) expect(outcome.sentence).not.toBe('');
	});

	test('a network failure is a refusal the row can render, never a thrown error', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn(async () => {
				throw new TypeError('Failed to fetch');
			})
		);

		const outcome = await postControl({ action: 'start', target: 'gitea', ack: '' });

		expect(outcome.ok).toBe(false);
	});
});

describe('fetchControlStatus', () => {
	test('refuses a response that is not the control view', async () => {
		respondWith(200, { ok: true });

		const result = await fetchControlStatus();

		expect(result.ok).toBe(false);
	});

	test('reports the status code when the plane is not installed', async () => {
		respondWith(503, { code: 'unavailable', sentence: 'The control plane is not installed.' });

		const result = await fetchControlStatus();

		expect(result).toEqual({ ok: false, error: 'control status 503' });
	});
});
