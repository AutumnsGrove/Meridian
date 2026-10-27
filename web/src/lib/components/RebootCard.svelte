<script lang="ts">
	import type { ArmState } from '#lib/state/armed';
	import ActionButton from './ActionButton.svelte';

	let {
		reason,
		arm = { phase: 'idle' },
		isActive = false,
		onTap,
		onCancel
	}: {
		reason: string;
		/** The page's one armed-button machine. */
		arm?: ArmState;
		/** True when the machine belongs to this card rather than a service row. */
		isActive?: boolean;
		onTap?: (action: string) => void;
		onCancel?: () => void;
	} = $props();

	const isArmed = $derived(arm.phase === 'armed' && isActive);
	const isSending = $derived(isActive && (arm.phase === 'sending' || arm.phase === 'pending'));
</script>

<section class="host" aria-labelledby="host-title">
	<h2 id="host-title">Host</h2>
	<div class="card">
		<h3>Reboot this box</h3>
		<p class="note">
			It goes offline for about 40 seconds and comes back on its own. The boot health check emails
			you a report when it is back.
		</p>
		<span class="visually-hidden" id="why-reboot">{reason}</span>

		{#if isArmed}
			<div class="armed" aria-live="polite">
				<p class="warning">
					The launcher will be offline for about 40s. Everything comes back on its own.
				</p>
				<div class="actions">
					<ActionButton
						label="cancel"
						reason=""
						describedBy="why-reboot"
						onclick={() => onCancel?.()}
					/>
					<ActionButton
						label="reboot anyway"
						reason=""
						destructive
						describedBy="why-reboot"
						onclick={() => onTap?.('reboot')}
					/>
				</div>
			</div>
		{:else}
			<div class="actions">
				<ActionButton
					label="reboot…"
					{reason}
					busy={isSending ? 'rebooting… queued' : ''}
					destructive
					describedBy="why-reboot"
					onclick={() => onTap?.('reboot')}
				/>
			</div>
		{/if}
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

	.armed {
		margin-top: 12px;
		padding: 12px;
		background: var(--down-soft);
		border: 1px solid color-mix(in srgb, var(--status-down) 35%, transparent);
		border-radius: var(--radius);
	}

	.warning {
		font-size: 13px;
	}

	.armed .actions :global(.button:first-child) {
		flex: 2 1 120px;
		background: var(--color-accent);
		border-color: var(--color-accent);
		color: var(--color-on-accent);
		font-weight: 500;
	}
</style>
