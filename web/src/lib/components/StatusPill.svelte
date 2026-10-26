<script lang="ts">
	import type { ServiceStatus } from '#lib/api/types';
	import { statusWord } from '#lib/state/status';
	import StatusMark from './StatusMark.svelte';

	let { status }: { status: ServiceStatus } = $props();
</script>

<span class="pill">
	<StatusMark {status} showLabel={false} />
	<span class="word">{statusWord(status)}</span>
</span>

<style>
	.pill {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		min-height: 28px;
		padding: 0 11px 0 8px;
		font-size: var(--fs-meta);
		white-space: nowrap;
		background: var(--color-surface-2);
		border: 1px solid var(--color-border);
		border-radius: 999px;
	}
</style>
