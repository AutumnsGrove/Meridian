import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { expect, expectTapTarget, test } from './fixture';

// The control page with a working plane. The e2e run starts the stub executor (testdata/stub) against
// this run's control directory, so a tap goes all the way: POST, queue file, executor, result, audit
// line, page. Nothing here needs root, systemd or Docker.

test.beforeEach(async ({ visit, page }) => {
	await visit('/control');
	await expect(page.getByRole('heading', { level: 2, name: 'Gitea' })).toBeVisible();
});

/** The row for a service, because every row has the same button names. */
function row(page: Page, name: string) {
	return page
		.getByRole('list', { name: 'Services' })
		.getByRole('listitem')
		.filter({
			has: page.getByRole('heading', { level: 2, name })
		});
}

test('acts, follows the action and keeps the receipt', async ({ page }) => {
	const gitea = row(page, 'Gitea');

	await expect(gitea.getByRole('button', { name: 'restart' })).toBeEnabled();
	await gitea.getByRole('button', { name: 'restart' }).click();

	// In flight first: the row says what is happening before the executor has answered.
	await expect(gitea.getByText(/restarting/)).toBeVisible();

	// Then the result, and the history line that proves the action happened.
	await expect(gitea.getByText(/restarted in \d/)).toBeVisible();
	await expect(
		page.getByRole('region', { name: 'Action history' }).getByText('restart gitea')
	).toBeVisible();
});

test('arms a DNS stop, warns, and cancels it again', async ({ page }) => {
	const metrics = row(page, 'Metrics');

	await metrics.getByRole('button', { name: 'stop' }).click();

	// Nothing is sent by the first tap: the row explains the consequence instead.
	await expect(metrics.getByText(/breaks DNS for the whole tailnet/)).toBeVisible();
	await expect(metrics.getByRole('button', { name: 'cancel' })).toBeVisible();

	await metrics.getByRole('button', { name: 'cancel' }).click();
	await expect(metrics.getByText(/breaks DNS for the whole tailnet/)).toBeHidden();
});

test('a completed DNS stop counts down, and its cancel ends the countdown', async ({ page }) => {
	const metrics = row(page, 'Metrics');

	await metrics.getByRole('button', { name: 'stop' }).click();
	await metrics.getByRole('button', { name: 'stop anyway' }).click();

	// The executor reported a scheduled restore, so the page shows how long the stop has left.
	const countdown = page.getByText(/comes back in \d+:\d\d unless you cancel it/);
	await expect(countdown).toBeVisible();

	await page.getByRole('button', { name: 'cancel auto-restore' }).click();
	await expect(countdown).toBeHidden();
});

test('a reboot writes its result before it happens and the page comes back', async ({ page }) => {
	// The control directory is shared by every test in the run, so a previous reboot's row would let
	// this pass without doing anything: count, then require the count to grow.
	const rows = page.getByRole('region', { name: 'Action history' }).getByText('reboot host');
	const before = await rows.count();

	await page.getByRole('button', { name: 'reboot…' }).click();

	// The first tap only arms it.
	await expect(page.getByText(/offline for about 40s/).first()).toBeVisible();
	await page.getByRole('button', { name: 'reboot anyway' }).click();

	// The provisional result is what the page shows while the box is away; the stub holds it for a
	// moment so this is visible rather than a race.
	await expect(page.getByText('Rebooting. Back in about 40s.')).toBeVisible();

	// The hub answers, the page asks again, and the finished action replaces the banner.
	await expect(rows).toHaveCount(before + 1, { timeout: 15_000 });
	await expect(page.getByText('Rebooting. Back in about 40s.')).toBeHidden();
});

test('every button on the page is a 44px tap target', async ({ page }) => {
	const buttons = page.getByRole('button');
	const count = await buttons.count();
	expect(count).toBeGreaterThan(0);
	for (let index = 0; index < count; index++) {
		await expectTapTarget(buttons.nth(index));
	}
});

test('the way back to the launcher works', async ({ page }) => {
	await page.getByRole('link', { name: /Meridian/ }).click();
	await expect(page).toHaveURL(/\/$/);
	await expect(page.getByRole('searchbox', { name: 'Search anything' })).toBeVisible();
});

test('has no accessibility violations', async ({ page }) => {
	const report = await new AxeBuilder({ page }).analyze();
	expect(report.violations.map((violation) => `${violation.id}: ${violation.help}`)).toEqual([]);
});
