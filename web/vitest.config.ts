import { svelte, vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

// Unit tests run without SvelteKit's dev server: a plain Svelte plugin, jsdom, and the same #lib
// subpath import the app uses. SvelteKit's own config is not loaded: only the compiler is needed.
export default defineConfig({
	plugins: [svelte({ configFile: false, preprocess: vitePreprocess() })],
	resolve: {
		alias: { '#lib': fileURLToPath(new URL('./src/lib', import.meta.url)) },
		// 'browser' first so Svelte resolves its client runtime; 'node' stays because Vite's own
		// runtime imports node: builtins.
		conditions: ['browser', 'node']
	},
	test: {
		environment: 'jsdom',
		include: ['src/**/*.test.ts'],
		setupFiles: ['./vitest-setup.ts']
	}
});
