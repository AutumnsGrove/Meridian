import AxeBuilder from '@axe-core/playwright';
import { expect, expectTapTarget, test } from './fixture';

// The control page in Phase 1: a real page with real rows and nothing that can act. Every button is
// disabled for the same reason, and the reason is on the page (docs/DESIGN.md "Control panel").

test.beforeEach(async ({ visit }) => {
	await visit('/control');
});

test('lists the controllable services and says why nothing works yet', async ({ page }) => {
	await expect(page.getByRole('heading', { level: 1, name: 'Control' })).toBeVisible();
	await expect(page.getByText('Control plane not installed on this box')).toBeVisible();
	await expect(page.getByRole('list', { name: 'Services' }).getByRole('listitem')).toHaveCount(2);
	await expect(page.getByRole('heading', { level: 2, name: 'Gitea' })).toBeVisible();
	await expect(page.getByRole('heading', { level: 2, name: 'Metrics' })).toBeVisible();
});

test('every action is visible, disabled, and says why', async ({ page }) => {
	const buttons = page.getByRole('button');
	// Three actions on each of the two rows, plus the host reboot.
	await expect(buttons).toHaveCount(7);

	const count = await buttons.count();
	for (let index = 0; index < count; index++) {
		const button = buttons.nth(index);
		await expect(button).toBeDisabled();
		await expect(button).toHaveAttribute('aria-describedby', /.+/);
		await expectTapTarget(button);
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
