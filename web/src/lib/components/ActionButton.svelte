<script lang="ts">
	let {
		label,
		reason,
		detail = '',
		busy = '',
		destructive = false,
		describedBy,
		onclick
	}: {
		label: string;
		reason: string;
		detail?: string;
		/** What the button says while its request is in flight. */
		busy?: string;
		/** A stop or a reboot: outlined in rose, never filled (docs/DESIGN.md "Control panel"). */
		destructive?: boolean;
		describedBy: string;
		onclick?: () => void;
	} = $props();

	// A reason is the whole rule: a button that carries one is refused, and one that carries none is
	// the control plane's to run.
	const isDisabled = $derived(reason !== '' || detail !== '' || busy !== '');
	const detailID = $derived(`${describedBy}-detail`);
	const described = $derived(
		[reason !== '' ? describedBy : '', detail !== '' ? detailID : ''].filter(Boolean).join(' ') ||
			undefined
	);
</script>

<button
	type="button"
	class="button"
	class:destructive
	class:busy={busy !== ''}
	disabled={isDisabled}
	aria-describedby={described}
	onclick={() => onclick?.()}
>
	<span class="label">{busy !== '' ? busy : label}</span>
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

	/* A destructive button is outlined, never filled, so it cannot read as the safe choice. */
	.destructive:not(:disabled) {
		color: var(--status-down);
		border-color: color-mix(in srgb, var(--status-down) 55%, var(--color-border-strong));
	}

	.busy {
		color: var(--color-text-dim);
		border-style: dashed;
	}

	.detail {
		font-size: 11px;
		font-weight: 400;
		color: var(--color-text-dim);
	}
</style>
