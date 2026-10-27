<script lang="ts">
	import type { AutoRestore } from '#lib/api/control';
	import { countdownLabel, remainingSeconds } from '#lib/state/control';
	import ActionButton from './ActionButton.svelte';

	let {
		restore,
		name,
		now,
		onCancel
	}: {
		restore: AutoRestore;
		/** The service name for the target, or the slug when the config has no row for it. */
		name: string;
		/** The browser's clock, so the countdown agrees with every other age on the page. */
		now: number;
		onCancel?: (ref: string) => void;
	} = $props();

	const seconds = $derived(remainingSeconds(restore, now));
</script>

<section class="restore" aria-live="polite">
	<p class="text">
		{name} comes back in <span class="countdown">{countdownLabel(seconds)}</span> unless you cancel it.
	</p>
	<div class="actions">
		<ActionButton
			label="cancel auto-restore"
			reason=""
			describedBy="why-restore"
			onclick={() => onCancel?.(restore.ref)}
		/>
	</div>
	<span class="visually-hidden" id="why-restore">
		Cancelling leaves {name} stopped until someone starts it again.
	</span>
</section>

<style>
	.restore {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 12px 14px;
		background: var(--warn-soft);
		border-left: 3px solid var(--status-degraded);
		border-radius: var(--radius);
	}

	.text {
		font-size: var(--fs-meta);
	}

	.countdown {
		font-variant-numeric: tabular-nums;
		font-weight: 500;
	}

	.actions {
		display: flex;
		gap: 8px;
	}

	.actions :global(.button) {
		flex: 0 1 auto;
		min-width: 148px;
	}
</style>
