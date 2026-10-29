import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

type SvelteKitOptions = Parameters<typeof sveltekit>[0];

// The app is a single-page app served by Go: one index.html fallback, nothing rendered or
// prerendered on a server. The CSP is hash based because there is no request to mint a nonce from
// (docs/STANDARDS.md section 6, decision #58).
const csp: NonNullable<SvelteKitOptions>['csp'] = {
	mode: 'hash',
	directives: {
		'default-src': ['self'],
		'script-src': ['self'],
		// SvelteKit's route announcer (a live region for screen readers) is created with a hardcoded
		// inline `style` attribute, which a hash-mode policy blocks on every page load: an error in the
		// console and a live region that is not clipped. Scripts stay hash-only, which is the part that
		// matters; the app sets no inline styles of its own (decision #77).
		'style-src': ['self', 'unsafe-inline'],
		'font-src': ['self'],
		'img-src': ['self', 'data:'],
		'connect-src': ['self'],
		'object-src': ['none'],
		'base-uri': ['self'],
		'form-action': ['none'],
		'frame-ancestors': ['none']
	}
};

// In `vite dev` the API and icons come from a running `meridian` binary on loopback, so the browser
// stays same-origin and the host guard sees a loopback Host.
const backend = 'http://127.0.0.1:8891';

export default defineConfig({
	plugins: [
		sveltekit({
			adapter: adapter({ fallback: 'index.html' }),
			csp
		})
	],
	// Kit externalises its own dependencies in the SSR output, and adapter-static still imports it
	// once to analyse routes. pnpm does not link Kit's dependencies into the app, so `cookie` cannot
	// be resolved from here (a stray parent node_modules is worse: it resolves an old CommonJS
	// copy). Bundling it into the analysed chunk removes the lookup.
	ssr: { noExternal: ['cookie'] },
	server: {
		proxy: {
			'/api': backend,
			'/icons': backend,
			'/healthz': backend
		}
	}
});
