import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import tseslint from 'typescript-eslint';

export default tseslint.config(
	{
		ignores: ['build/', '.svelte-kit/', 'node_modules/', 'playwright-report/', 'test-results/']
	},
	js.configs.recommended,
	...tseslint.configs.recommended,
	...svelte.configs.recommended,
	{
		files: ['**/*.svelte', '**/*.svelte.ts', '**/*.svelte.js'],
		languageOptions: {
			// The plugin's config does not carry browser globals; name the ones the app uses.
			globals: {
				window: 'readonly',
				document: 'readonly',
				fetch: 'readonly',
				setTimeout: 'readonly',
				clearTimeout: 'readonly'
			},
			parserOptions: { parser: tseslint.parser }
		},
		rules: {
			// Links are absolute from / because the gateway serves the SPA there with no base path.
			// A base path would need resolve() from $app/paths instead.
			'svelte/no-navigation-without-resolve': 'off'
		}
	}
);
