import { render, screen } from '@testing-library/svelte';
import { expect, test } from 'vitest';
import Page from './+page.svelte';

// The smoke test: a component compiles through the Svelte plugin, mounts in jsdom, and is found by
// role. If any piece of the test toolchain breaks, this is the test that says so.
test('the launcher route renders a page heading', () => {
	render(Page);

	expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Meridian');
});
