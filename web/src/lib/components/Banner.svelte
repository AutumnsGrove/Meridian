<script lang="ts">
	let { text, tone = 'error' }: { text: string; tone?: 'error' | 'warn' } = $props();
</script>

<p class="banner banner-{tone}" role="status">{text}</p>

<style>
	.banner {
		padding: 10px 14px;
		background: var(--color-surface-2);
		border-left: 3px solid var(--color-text-faint);
		border-radius: var(--radius);
		color: var(--color-text);
		font-size: var(--fs-meta);
	}

	.banner-error {
		border-left-color: var(--status-down);
	}

	.banner-warn {
		border-left-color: var(--status-degraded);
	}
</style>
