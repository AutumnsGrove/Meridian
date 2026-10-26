<script lang="ts">
	let {
		label,
		reason,
		detail = '',
		describedBy
	}: { label: string; reason: string; detail?: string; describedBy: string } = $props();

	// A reason is the whole rule: a button that carries one is refused, and one that carries none is
	// the control plane's to run (docs/DESIGN.md "Control panel").
	const isDisabled = $derived(reason !== '' || detail !== '');
	const detailID = $derived(`${describedBy}-detail`);
	const described = $derived(
		[reason !== '' ? describedBy : '', detail !== '' ? detailID : ''].filter(Boolean).join(' ') ||
			undefined
	);
</script>

<button type="button" class="button" disabled={isDisabled} aria-describedby={described}>
	<span class="label">{label}</span>
	{#if detail}
		<span class="detail" id={detailID}>{detail}</span>
	{/if}
</button>

<style>
	.button {
		display: inline-flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 2px;
		flex: 1 1 88px;
		min-height: var(--touch);
		padding: 8px 14px;
		font-family: var(--font-ui);
		font-size: 14px;
		line-height: 1.2;
		color: var(--color-text);
		text-align: center;
		background: var(--color-surface-2);
		border: 1px solid var(--color-border-strong);
		border-radius: 8px;
		cursor: pointer;
	}

	.button:hover:not(:disabled) {
		background: var(--color-surface-2);
		border-color: var(--color-accent);
	}

	/* Refused buttons stay visible and say why, at 40% opacity, never hidden. */
	.button:disabled {
		opacity: 0.4;
		cursor: not-allowed;
	}

	.detail {
		font-size: 11px;
		font-weight: 400;
		color: var(--color-text-dim);
	}
</style>
