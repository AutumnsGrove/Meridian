import { defineConfig } from '@playwright/test';

/**
 * The end-to-end suite: the real `meridian` binary, the app it embeds, the portable fixture, and the
 * stub upstream in `testdata/stub`. `globalSetup` starts all of it on free ports and warms the first
 * poll, so every spec begins from a settled page.
 *
 * Two viewports, because the phone one decides the layout (docs/DESIGN.md): 390x844 is the project
 * that must pass first, and 1440x900 only has to not get worse.
 */
export default defineConfig({
	testDir: 'test/e2e',
	globalSetup: './test/e2e/global-setup.ts',
	// One worker, deliberately: there is one binary and one stub, and the flip test changes shared
	// state. Running settles in sequence also keeps the poll timings honest.
	workers: 1,
	fullyParallel: false,
	forbidOnly: Boolean(process.env.CI),
	retries: process.env.CI ? 1 : 0,
	reporter: 'list',
	// A poll cycle is 20s and one test waits for it, so the default 30s is too tight.
	timeout: 90_000,
	expect: { timeout: 10_000 },
	use: { trace: 'retain-on-failure', screenshot: 'only-on-failure' },
	projects: [
		{ name: 'phone', use: { browserName: 'chromium', viewport: { width: 390, height: 844 } } },
		{ name: 'desktop', use: { browserName: 'chromium', viewport: { width: 1440, height: 900 } } }
	]
});
