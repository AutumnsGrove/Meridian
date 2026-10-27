import { render, screen } from '@testing-library/svelte';
import { describe, expect, test } from 'vitest';
import { loadStatus, serviceNamed } from '../../../test/golden';
import Section from './Section.svelte';
import Banner from './Banner.svelte';

const status = loadStatus('status.all-ok.golden.json');
const now = Date.parse(status.generated_at);

describe('Section', () => {
	test('heads the section and lists every service in it, in the order the server sent them', () => {
		const section = status.sections[0];

		render(Section, { section, now });

		expect(screen.getByRole('heading', { level: 2, name: 'Media' })).toBeTruthy();
		expect(screen.getByRole('link', { name: 'Jellyfin healthy films and shows' })).toBeTruthy();
		expect(screen.getByText('Metrics')).toBeTruthy();
		expect(screen.getByText('peers')).toBeTruthy();
		expect(screen.getByText('3 online')).toBeTruthy();
	});

	test('renders nothing at all for a section with no services', () => {
		const { container } = render(Section, {
			section: { id: 'empty', title: 'Nothing here', services: [] },
			now
		});

		expect(container.textContent?.trim()).toBe('');
		expect(screen.queryByRole('heading')).toBeNull();
	});

	test('draws the portable fixture, so no service name is hard-coded to make it look right', () => {
		const dev = status.sections[1];

		render(Section, { section: dev, now });

		expect(screen.getByRole('heading', { name: 'Development' })).toBeTruthy();
		expect(screen.getByText('Gitea')).toBeTruthy();
		expect(screen.getByText(serviceNamed(status, 'Backup daemon').description)).toBeTruthy();
	});
});

describe('Banner', () => {
	test('is announced as a status, with the sentence it was given', () => {
		render(Banner, { text: "Can't reach Meridian" });

		// Whitespace between the sentences of a status region is markup, not copy.
		expect(screen.getByRole('status').textContent?.trim()).toBe("Can't reach Meridian");
	});

	test('carries the tone as a class, so a warning and an error do not look the same', () => {
		const { container } = render(Banner, { text: 'Config rejected', tone: 'warn' });

		expect(container.querySelector('.banner')?.className).toContain('banner-warn');
	});
});
