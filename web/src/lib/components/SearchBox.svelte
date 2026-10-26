<script lang="ts">
	import type { SearchButton } from '#lib/api/types';
	import { buildSearchURL } from '#lib/format/search';

	let {
		buttons,
		open = (url: string) => window.location.assign(url)
	}: { buttons: SearchButton[]; open?: (url: string) => void } = $props();

	let query = $state('');

	// Empty input disables every button rather than doing something surprising (DESIGN "Search").
	const isEmpty = $derived(query.trim() === '');

	function submit(index: number): void {
		if (isEmpty) return;
		const button = buttons[index];
		if (!button) return;
		open(buildSearchURL(button.url_template, query));
	}
</script>

<form
	class="search"
	role="search"
	onsubmit={(event) => {
		event.preventDefault();
		submit(0);
	}}
>
	<input
		class="field touch-target"
		type="search"
		aria-label="Search anything"
		placeholder="Search anything…"
		autocomplete="off"
		spellcheck="false"
		bind:value={query}
	/>
	<div class="buttons">
		{#each buttons as button, index (button.label)}
			<button
				class="button touch-target"
				type={index === 0 ? 'submit' : 'button'}
				disabled={isEmpty}
				onclick={() => submit(index)}>{button.label}</button
			>
		{/each}
	</div>
</form>

<style>
	.search {
		display: flex;
		flex-direction: column;
		gap: 8px;
		width: 100%;
		max-width: 560px;
	}

	.field {
		width: 100%;
		padding: 0 14px;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		color: var(--color-text);
		font-family: var(--font-ui);
		font-size: var(--fs-name);
	}

	.field::placeholder {
		color: var(--color-text-faint);
	}

	.buttons {
		display: flex;
		gap: 8px;
	}

	.button {
		flex: 1;
		padding: 0 14px;
		background: var(--color-surface-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		color: var(--color-text);
		font-family: var(--font-ui);
		font-size: var(--fs-name);
		cursor: pointer;
	}

	.button:hover:not(:disabled) {
		border-color: var(--color-accent);
	}

	.button:disabled {
		color: var(--color-text-faint);
		cursor: not-allowed;
	}
</style>
