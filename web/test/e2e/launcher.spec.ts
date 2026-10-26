import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import {
	drawnColor,
	expect,
	expectTapTarget,
	SEARCH_ORIGIN,
	test,
	tiles,
	tokenColor
} from './fixture';
import { flip, refresh } from './upstream';

/** The launcher. Everything here is what a phone does with it in the first ten seconds. */
test.beforeEach(async ({ visit }) => {
	await visit('/');
});

test('renders both fixture sections and every service', async ({ page }) => {
	await expect(page.getByRole('heading', { name: 'Media' })).toBeVisible();
	await expect(page.getByRole('heading', { name: 'Development' })).toBeVisible();
	await expect(tiles(page)).toHaveCount(4);
	for (const name of ['Gitea', 'Jellyfin', 'Backup daemon', 'Metrics']) {
		await expect(page.getByText(name, { exact: true })).toBeVisible();
	}
	await expect(page.getByText('4/4 up')).toBeVisible();
});

test('gives every tile and control a 44px tap target', async ({ page }) => {
	const boxes = await tiles(page).evaluateAll((elements) =>
		elements.map((element) => {
			const box = element.getBoundingClientRect();
			return { width: box.width, height: box.height };
		})
	);
	expect(boxes).toHaveLength(4);
	for (const box of boxes) {
		expect(box.height).toBeGreaterThanOrEqual(44);
		expect(box.width).toBeGreaterThanOrEqual(44);
	}

	for (const target of [
		page.getByRole('searchbox', { name: 'Search anything' }),
		page.getByRole('button', { name: 'Search the web' }),
		page.getByRole('link', { name: 'Control panel' })
	]) {
		await expectTapTarget(target);
	}
});

test('does not scroll sideways', async ({ page }) => {
	expect(await sidewaysOverflow(page)).toBeLessThanOrEqual(0);
});

test('stays readable at 200% zoom on a phone', async ({ page }) => {
	// 200% zoom on a 390px phone leaves 195 CSS pixels to lay the page out in.
	await page.setViewportSize({ width: 195, height: 422 });
	expect(await sidewaysOverflow(page)).toBeLessThanOrEqual(0);
});

test('search buttons stay disabled until there is something to search for', async ({ page }) => {
	const search = page.getByRole('searchbox', { name: 'Search anything' });
	const button = page.getByRole('button', { name: 'Search the web' });

	await expect(button).toBeDisabled();
	await search.fill('homelab');
	await expect(button).toBeEnabled();
	await search.fill('   ');
	await expect(button).toBeDisabled();
});

test('Enter submits the first button and lands on the search URL', async ({ page }) => {
	await page.route(`${SEARCH_ORIGIN}/**`, (route) =>
		route.fulfill({
			status: 200,
			contentType: 'text/html',
			body: '<!doctype html><title>search</title><p>results</p>'
		})
	);

	await page.getByRole('searchbox', { name: 'Search anything' }).fill('hello world');
	await page.getByRole('searchbox', { name: 'Search anything' }).press('Enter');

	await expect(page).toHaveURL(`${SEARCH_ORIGIN}/?q=hello%20world`);
});

test('tab reaches every control and each one shows its focus ring', async ({ page }) => {
	// A disabled button is not in the tab order, so the search button needs a query before it can be
	// reached. The field keeps the focus, so the walk starts at the next control and wraps around.
	await page.getByRole('searchbox', { name: 'Search anything' }).fill('homelab');

	const expected = await page.evaluate(
		() => document.querySelectorAll('a[href], button:not([disabled]), input:not([disabled])').length
	);
	expect(expected).toBe(5);

	const reached: string[] = [];
	// One extra press: the browser leaves the document between the last control and the first.
	for (let press = 0; press < expected + 1 && reached.length < expected; press++) {
		await page.keyboard.press('Tab');
		const focused = await page.evaluate(() => {
			const element = document.activeElement;
			if (element === null || element === document.body) return null;
			const style = getComputedStyle(element);
			return {
				name: element.getAttribute('aria-label') ?? element.textContent?.trim() ?? '',
				outline: `${style.outlineStyle} ${style.outlineWidth} ${style.outlineOffset}`
			};
		});
		if (focused === null) continue;
		// 2px solid in the accent, offset 2px (docs/DESIGN.md "Accessibility and other constraints").
		expect(focused.outline, `the focus ring on ${focused.name}`).toBe('solid 2px 2px');
		if (!reached.includes(focused.name)) reached.push(focused.name);
	}

	expect(reached).toHaveLength(expected);
	for (const pattern of [
		/^Search anything$/,
		/^Search the web$/,
		/^Jellyfin/,
		/^Gitea/,
		/^Control panel/
	]) {
		expect(
			reached.some((name) => pattern.test(name)),
			`nothing matched ${pattern}`
		).toBe(true);
	}
});

test('has no accessibility violations', async ({ page }) => {
	const report = await new AxeBuilder({ page }).analyze();
	// TEMP DEBUG
	console.log(
		JSON.stringify(
			report.violations.map((violation) => ({
				id: violation.id,
				nodes: violation.nodes.map((node) => node.target)
			})),
			null,
			2
		)
	);
	expect(report.violations.map((violation) => `${violation.id}: ${violation.help}`)).toEqual([]);
});

test('a service that goes down offers a way into its control row', async ({ run, page }) => {
	// The page is already open: no reload, because the poll loop noticing on its own is the
	// behaviour under test. One cycle is 20s, so the wait is deliberately generous.
	const tile = page.getByRole('link', { name: /^Gitea healthy/ });
	await expect(tile).toBeVisible();

	await flip(run, 'gitea', 'down');
	try {
		const down = page.getByRole('link', { name: /^Gitea down/ });
		await expect(down).toBeVisible({ timeout: 45_000 });
		await expect(down).toContainText('✕');
		await expect(down).toContainText('restart →');
		await expect(down).toHaveAttribute('href', '/control#gitea');

		await down.click();
		await expect(page).toHaveURL(/\/control#gitea$/);
		const row = page.locator('li#gitea');
		await expect(row).toBeVisible();
		await expect(row).toHaveClass(/highlighted/);
	} finally {
		await flip(run, 'gitea', 'up');
		await refresh(run);
	}
});

test('keeps an old vital, and dims it instead of hiding it', async ({ page, visit }) => {
	// Five minutes of real waiting per vital would make this suite useless, so the browser's clock
	// moves instead: the value, its age and its dimming all come from that one clock (decision #76).
	// The clock has to be installed before the timers it drives exist, so the page is opened again.
	await page.clock.install({ time: new Date() });
	await visit('/');
	const vital = tiles(page).filter({ hasText: 'Metrics' }).locator('.vital');
	await expect(vital).toContainText('2 online');

	const fresh = await drawnColor(vital);
	await page.clock.fastForward('06:00');

	await expect(vital).toHaveClass(/vital-stale/);
	await expect(vital).toContainText('2 online · 6m ago');
	const old = await drawnColor(vital);
	expect(old).not.toBe(fresh);
	expect(old).toBe(await tokenColor(page, '--color-text-faint'));
});

/** How many pixels wider the document is than the window, at the current viewport. */
async function sidewaysOverflow(page: Page): Promise<number> {
	return page.evaluate(
		() => document.documentElement.scrollWidth - document.documentElement.clientWidth
	);
}
