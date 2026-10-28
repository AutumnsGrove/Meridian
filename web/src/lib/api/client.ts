import type { ParseOutcome } from './fields';
import { parseControlStatus, type ControlStatus } from './control';
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

/** A tap's outcome: the queued id, or the refusal the container explained. */
export type ControlTapOutcome =
	{ ok: true; id: string } | { ok: false; code: string; sentence: string };

/** What the page sends: a choice among what the config already declares, and nothing else. */
export type ControlTap = {
	action: string;
	target: string;
	ack: string;
	ref?: string;
};

const UNREACHABLE = 'The hub did not answer. Check the connection and try again.';

/**
 * Queues one tap: the answer is the request id, and the page follows the result through the status
 * route. A self-update resolves its digest first, so that one tap may take longer than the rest; a
 * refusal comes back in the container's own words (decision #86), so the page never keeps a copy of
 * the code table.
 */
export async function postControl(tap: ControlTap): Promise<ControlTapOutcome> {
	try {
		const response = await fetch('/api/control', {
			method: 'POST',
			headers: { 'content-type': 'application/json', accept: 'application/json' },
			body: JSON.stringify(tap),
			cache: 'no-store',
			credentials: 'same-origin',
			redirect: 'error'
		});
		const body: unknown = await response.json().catch(() => null);
		if (response.ok) {
			const id = idFrom(body);
			return id === null
				? { ok: false, code: 'internal', sentence: UNREACHABLE }
				: { ok: true, id };
		}
		return refusalFrom(body);
	} catch {
		return { ok: false, code: 'internal', sentence: UNREACHABLE };
	}
}

function idFrom(body: unknown): string | null {
	if (typeof body !== 'object' || body === null) return null;
	const id = (body as { [key: string]: unknown }).id;
	return typeof id === 'string' && id !== '' ? id : null;
}

function refusalFrom(body: unknown): ControlTapOutcome {
	if (typeof body === 'object' && body !== null) {
		const fields = body as { [key: string]: unknown };
		if (typeof fields.code === 'string' && typeof fields.sentence === 'string') {
			return { ok: false, code: fields.code, sentence: fields.sentence };
		}
	}
	return { ok: false, code: 'internal', sentence: UNREACHABLE };
}

/** Fetches and validates GET /api/control/status; it never throws. */
export async function fetchControlStatus(): Promise<ParseOutcome<ControlStatus>> {
	try {
		const response = await fetch('/api/control/status', {
			headers: { accept: 'application/json' },
			cache: 'no-store',
			credentials: 'same-origin',
			redirect: 'error'
		});
		if (!response.ok) return { ok: false, error: `control status ${response.status}` };
		return parseControlStatus(await response.json());
	} catch (error) {
		return { ok: false, error: error instanceof Error ? error.message : 'request failed' };
	}
}
