import { render, screen } from '@testing-library/svelte';
import { describe, expect, test } from 'vitest';
import { loadStatus, serviceNamed } from '../../../test/golden';
import type { ServiceStatus } from '#lib/api/types';
import StatusMark from './StatusMark.svelte';

const WORDS: Record<ServiceStatus, string> = {
	ok: 'healthy',
	degraded: 'degraded',
	down: 'down',
	unknown: 'unknown',
	disabled: 'disabled'
};

describe('StatusMark', () => {
	test.each(Object.keys(WORDS) as ServiceStatus[])(
		'draws %s as a glyph plus a hidden word',
		(status) => {
			const { container } = render(StatusMark, { status });

			const glyph = container.querySelector('.mark');
			expect(glyph?.getAttribute('aria-hidden')).toBe('true');
			expect(glyph?.textContent?.trim()).not.toBe('');
			expect(screen.getByText(WORDS[status]).className).toContain('visually-hidden');
		}
	);

	test('gives the five statuses five different glyphs', () => {
		const glyphs = (Object.keys(WORDS) as ServiceStatus[]).map((status) => {
			const { container, unmount } = render(StatusMark, { status });
			const glyph = container.querySelector('.mark')?.textContent?.trim();
			unmount();
			return glyph;
		});

		expect(new Set(glyphs).size).toBe(5);
	});

	test('is independent of the data it decorates', () => {
		const status = loadStatus('status.one-down.golden.json');
		const jellyfin = serviceNamed(status, 'Jellyfin');

		render(StatusMark, { status: jellyfin.status });

		expect(screen.getByText('down')).toBeTruthy();
	});
});
