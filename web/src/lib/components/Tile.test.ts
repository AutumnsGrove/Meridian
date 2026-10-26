import { render, screen } from '@testing-library/svelte';
import { describe, expect, test } from 'vitest';
import { loadStatus, serviceNamed } from '../../../test/golden';
import type { Vital } from '#lib/api/types';
import Tile from './Tile.svelte';

const status = loadStatus('status.all-ok.golden.json');
const now = Date.parse(status.generated_at);
const gitea = serviceNamed(status, 'Gitea');
const backup = serviceNamed(status, 'Backup daemon');

describe('Tile', () => {
	test('the whole tile is one link to the service, in the same tab', () => {
		render(Tile, { service: gitea, now });

		const link = screen.getByRole('link', { name: /^Gitea healthy version 1\.22\.0$/ });

		expect(link.getAttribute('href')).toBe('https://git.example.org');
		expect(link.getAttribute('rel')).toBe('noopener noreferrer');
		expect(link.getAttribute('target')).toBeNull();
	});

	test('a service with no URL is not a link at all, and still says what it is', () => {
		const { container } = render(Tile, { service: backup, now });
		const tile = screen.getByText('Backup daemon').closest('.tile');

		expect(screen.queryByRole('link')).toBeNull();
		expect(container.querySelector('.tile')).toBe(tile);
		expect(tile?.textContent).toContain('healthy');
		expect(tile?.textContent).toContain('no web surface at all');
	});

	test('a down service that can be restarted offers one link into its control row', () => {
		render(Tile, { service: { ...gitea, status: 'down' }, now });

		const link = screen.getByRole('link');

		expect(link.getAttribute('href')).toBe('/control#gitea');
		expect(link.textContent).toContain('restart →');
	});

	test('a down service that cannot be restarted has no restart affordance', () => {
		render(Tile, { service: { ...gitea, status: 'down', control: null }, now });

		expect(screen.queryByText('restart →')).toBeNull();
		expect(screen.getByRole('link').getAttribute('href')).toBe('https://git.example.org');
	});

	test('a healthy service never shows the restart affordance, even when it could restart', () => {
		render(Tile, { service: gitea, now });

		expect(screen.queryByText('restart →')).toBeNull();
	});

	test('shows the reason instead of a description once a service is down', () => {
		const down = loadStatus('status.one-down.golden.json');
		const jellyfin = serviceNamed(down, 'Jellyfin');

		render(Tile, { service: jellyfin, now });

		expect(screen.getByText(jellyfin.reason)).toBeTruthy();
	});

	test('a stale vital keeps its last value, with its age, dimmed', () => {
		const stale: Vital = {
			...gitea.vitals[0],
			at: new Date(now - 6 * 60_000).toISOString(),
			age_s: 360,
			failing: false
		};

		render(Tile, { service: { ...gitea, vitals: [stale] }, now });

		const value = screen.getByText('1.22.0 · 6m ago');
		expect(value.className).toContain('vital-stale');
	});

	test('a failing vital is dimmed and says how old its reading is', () => {
		const degraded = loadStatus('status.degraded-stale-vital.golden.json');
		const failing = serviceNamed(degraded, 'Gitea');

		render(Tile, { service: failing, now });

		const value = screen.getByText('1.22.0 · 9m ago');
		expect(value.className).toContain('vital-failed');
	});

	test.each([
		{ status: 'ok', word: 'healthy' },
		{ status: 'degraded', word: 'degraded' },
		{ status: 'down', word: 'down' },
		{ status: 'unknown', word: 'unknown' },
		{ status: 'disabled', word: 'disabled' }
	] as const)('a $status tile says "$word" to a screen reader', ({ status, word }) => {
		render(Tile, { service: { ...gitea, status }, now });

		expect(screen.getByText(word)).toBeTruthy();
	});
});
