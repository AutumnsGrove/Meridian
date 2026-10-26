import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, test, vi } from 'vitest';
import type { SearchButton } from '#lib/api/types';
import SearchBox from './SearchBox.svelte';

const buttons: SearchButton[] = [
	{ label: 'Search the web', url_template: 'https://search.example.org/?q={q}' },
	{ label: 'Ask the assistant', url_template: 'https://assistant.example.org/search?q={q}' }
];

describe('SearchBox', () => {
	test('every button is disabled until something is typed', async () => {
		render(SearchBox, { buttons, open: vi.fn() });

		for (const button of screen.getAllByRole('button')) {
			expect((button as HTMLButtonElement).disabled).toBe(true);
		}

		await fireEvent.input(screen.getByRole('searchbox'), { target: { value: 'ivy' } });

		for (const button of screen.getAllByRole('button')) {
			expect((button as HTMLButtonElement).disabled).toBe(false);
		}
	});

	test('whitespace alone does not count as something typed', async () => {
		render(SearchBox, { buttons, open: vi.fn() });

		await fireEvent.input(screen.getByRole('searchbox'), { target: { value: '   ' } });

		expect((screen.getAllByRole('button')[0] as HTMLButtonElement).disabled).toBe(true);
	});

	test('each button opens its own engine, with the query encoded', async () => {
		const open = vi.fn();
		render(SearchBox, { buttons, open });

		await fireEvent.input(screen.getByRole('searchbox'), { target: { value: 'a b' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Ask the assistant' }));

		expect(open).toHaveBeenCalledWith('https://assistant.example.org/search?q=a%20b');
	});

	test('submitting the form, which is what Enter does, uses the first button', async () => {
		const open = vi.fn();
		render(SearchBox, { buttons, open });

		await fireEvent.input(screen.getByRole('searchbox'), { target: { value: 'compost' } });
		await fireEvent.submit(screen.getByRole('search'));

		expect(open).toHaveBeenCalledWith('https://search.example.org/?q=compost');
	});

	test('the first button is the form default, so Enter is not a separate keybinding', () => {
		render(SearchBox, { buttons, open: vi.fn() });

		const [first, second] = screen.getAllByRole('button');

		expect(first.getAttribute('type')).toBe('submit');
		expect(second.getAttribute('type')).toBe('button');
	});

	test('nothing happens when the form is submitted empty', async () => {
		const open = vi.fn();
		render(SearchBox, { buttons, open });

		await fireEvent.submit(screen.getByRole('search'));

		expect(open).not.toHaveBeenCalled();
	});
});
