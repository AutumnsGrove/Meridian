<script lang="ts">
	let {
		text,
		detail = '',
		tone = 'error'
	}: { text: string; detail?: string; tone?: 'error' | 'warn' } = $props();
</script>

<div class="banner banner-{tone}" role="status">
	<p class="text">{text}</p>
	{#if detail}
		<p class="detail">{detail}</p>
	{/if}
</div>

<style>
	.banner {
		padding: 10px 14px;
		background: var(--color-surface-2);
		border-left: 3px solid var(--color-text-faint);
		border-radius: var(--radius);
		color: var(--color-text);
		font-size: var(--fs-meta);
	}

	.text {
		font-weight: 500;
	}

	.detail {
		margin-top: 2px;
		color: var(--color-text-dim);
	}

	.banner-error {
		border-left-color: var(--status-down);
	}

	.banner-warn {
		border-left-color: var(--status-degraded);
	}
</style>
