import { expect, test as base, type Locator, type Page } from '@playwright/test';
import { readRunState, type RunState } from './state';

// Every spec starts here rather than from raw `@playwright/test`: the page it loads is the real
// binary, and the three things worth failing on are the ones a user would notice. They are collected
// per test and asserted after it, which is what attributes a console warning to the test that caused
// it.

/**
 * The one origin a page may reach besides the app: the fixture's search button is an outward link to
 * a search engine, it is meant to leave, and the search test intercepts it so no request is sent.
 */
export const SEARCH_ORIGIN = 'https://search.example.org';

/** What the gateway must set on every response (docs/STANDARDS.md section 4). */
const REQUIRED_HEADERS = ['x-content-type-options', 'referrer-policy', 'x-frame-options'] as const;

type Watch = {
	/** Errors and warnings the page logged. */
	noise: string[];
	/** Uncaught exceptions in the page. */
	failures: string[];
	/** Requests that left the app's origin, other than the deliberate search link. */
	elsewhere: string[];
	/** Responses from the app that are missing one of the documented headers. */
	bare: string[];
};

/** Starts watching a page: console, exceptions, where requests go, and what they come back with. */
function watch(page: Page, appOrigin: string): Watch {
	const namespace = { noise: [], failures: [], elsewhere: [], bare: [] } as Watch;

	page.on('console', (message) => {
		if (message.type() === 'error' || message.type() === 'warning') {
			namespace.noise.push(`${message.type()}: ${message.text()}`);
		}
	});
	page.on('pageerror', (error) => namespace.failures.push(error.message));
	page.on('request', (request) => {
		const origin = new URL(request.url()).origin;
		if (origin !== appOrigin && origin !== SEARCH_ORIGIN) namespace.elsewhere.push(request.url());
	});
	page.on('response', (response) => {
		if (new URL(response.url()).origin !== appOrigin) return;
		const headers = response.headers();
		for (const name of REQUIRED_HEADERS) {
			if (!headers[name]) namespace.bare.push(`${name} missing from ${response.url()}`);
		}
	});

	return namespace;
}

/** Every tile on the launcher, in page order. The class is the tile component's own. */
export function tiles(page: Page): Locator {
	return page.locator('main .tile');
}

/** The colour an element is actually drawn in. */
export async function drawnColor(locator: Locator): Promise<string> {
	return locator.evaluate((element) => getComputedStyle(element).color);
}

/** What one of the design tokens resolves to on this page, so a colour can be named, not guessed. */
export async function tokenColor(page: Page, token: string): Promise<string> {
	return page.evaluate((name) => {
		const probe = document.createElement('span');
		probe.style.color = `var(${name})`;
		document.body.append(probe);
		const color = getComputedStyle(probe).color;
		probe.remove();
		return color;
	}, token);
}

/** Asserts one target is at least the 44px the design requires (docs/DESIGN.md). */
export async function expectTapTarget(locator: Locator): Promise<void> {
	const box = await locator.boundingBox();
	expect(box, 'the target has no box').not.toBeNull();
	expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
	expect(box?.width ?? 0).toBeGreaterThanOrEqual(44);
}

export const test = base.extend<{
	watch: Watch;
	run: RunState;
	visit: (path: string) => Promise<void>;
}>({
	watch: [
		async ({ page }, use) => {
			// The app's own origin is not known until the setup has written the state.
			const state = readRunState();
			const collected = watch(page, new URL(state.baseURL).origin);
			await use(collected);

			expect(collected.failures, 'the page threw').toEqual([]);
			expect(collected.noise, 'the page logged errors or warnings').toEqual([]);
			expect(collected.elsewhere, 'the page reached outside its own origin').toEqual([]);
			expect(collected.bare, 'a response was missing a security header').toEqual([]);
		},
		{ auto: true }
	],

	// A fixture with no dependencies still takes the fixtures argument: that is the signature.
	// eslint-disable-next-line no-empty-pattern
	run: async ({}, use) => {
		await use(readRunState());
	},

	/**
	 * Opens a page and waits for the first status response to be drawn. Both pages render nothing but
	 * a wordmark until then, so the tile list (or the control page's row list) is the readiness mark.
	 */
	visit: async ({ page, run }, use) => {
		await use(async (path: string) => {
			const response = await page.goto(run.baseURL + path);
			expect(response?.status(), `GET ${path}`).toBe(200);
			// The policy arrives as a meta tag because the app is static and there is no request to
			// mint a nonce from (decision #58), so the delivered document is where to look.
			expect(await response?.text(), `the CSP on ${path}`).toContain(
				'http-equiv="content-security-policy"'
			);
			await page.waitForFunction(() => document.querySelector('.tile, .rows') !== null);
		});
	}
});

export { expect };
