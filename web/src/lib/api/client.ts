import { parseStatus, type ParseResult } from './parse';

/**
 * Fetches and validates /api/status. The URL is relative, so the request is same-origin by
 * construction; no cookies are sent and the response is never cached by the browser.
 *
 * It never throws: every failure comes back as a refused ParseResult, which is what the poller
 * wants (it keeps the last good data and backs off).
 */
export async function fetchStatus(): Promise<ParseResult> {
	try {
		const response = await fetch('/api/status', {
			headers: { accept: 'application/json' },
			cache: 'no-store',
			credentials: 'same-origin',
			redirect: 'error'
		});
		if (!response.ok) return { ok: false, error: `status ${response.status}` };
		return parseStatus(await response.json());
	} catch (error) {
		return { ok: false, error: error instanceof Error ? error.message : 'request failed' };
	}
}
