<script lang="ts">
	import ActionButton from './ActionButton.svelte';

	let { reason }: { reason: string } = $props();
</script>

<section class="host" aria-labelledby="host-title">
	<h2 id="host-title">Host</h2>
	<div class="card">
		<h3>Reboot this box</h3>
		<p class="note">It goes offline for about 40 seconds and comes back on its own.</p>
		<span class="visually-hidden" id="why-reboot">{reason}</span>
		<div class="actions">
			<ActionButton label="reboot…" {reason} describedBy="why-reboot" />
		</div>
	</div>
</section>

<style>
	.host {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding-top: 28px;
		border-top: 1px solid var(--color-border);
	}

	h2 {
		font-size: var(--fs-meta);
		font-weight: 500;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--color-text-dim);
	}

	.card {
		padding: 16px;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	h3 {
		font-size: 16px;
		font-weight: 500;
	}

	.note {
		margin-top: 4px;
		font-size: 13px;
		color: var(--color-text-dim);
	}

	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin-top: 14px;
	}

	.actions :global(.button) {
		flex: 0 1 148px;
	}
</style>
