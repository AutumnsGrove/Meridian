<script lang="ts">
	import type { ControlStatus } from '#lib/api/control';
	import type { ControlAvailability, Service } from '#lib/api/types';
	import type { ArmState } from '#lib/state/armed';
	import {
		armedWarning,
		busyReason,
		controlReason,
		pendingFor,
		progressLabel,
		resultFor,
		resultLine,
		rowActions
	} from '#lib/state/control';
	import ActionButton from './ActionButton.svelte';
	import StatusPill from './StatusPill.svelte';

	let {
		service,
		control,
		view = null,
		arm = { phase: 'idle' },
		activeTarget = null,
		isHighlighted = false,
		onTap,
		onCancel
	}: {
		service: Service;
		control: ControlAvailability;
		/** The control view, null until the first answer arrives. */
		view?: ControlStatus | null;
		/** The page's one armed-button machine. */
		arm?: ArmState;
		/** The target that machine belongs to. */
		activeTarget?: string | null;
		isHighlighted?: boolean;
		onTap?: (action: string) => void;
		onCancel?: () => void;
	} = $props();

	const target = $derived(service.control?.target ?? service.id);
	const actions = $derived(rowActions(service, control));
	const whyID = $derived(`why-${service.id}`);
	const baseReason = $derived(
		control.available ? (view === null ? '' : busyReason(view, target)) : controlReason(control)
	);
	const isArmed = $derived(arm.phase === 'armed' && activeTarget === target);
	const armedAction = $derived(arm.phase === 'armed' ? arm.action : '');
	const pending = $derived(view === null ? null : pendingFor(view, target));
	const finished = $derived(view === null ? null : resultFor(view, target));

	// What this row's own button says while its request is the one in flight.
	function busyLabel(action: string): string {
		if (activeTarget !== target) return '';
		if ((arm.phase === 'sending' || arm.phase === 'pending') && arm.action === action) {
			return `${progressLabel(action)}${arm.phase === 'pending' ? ' queued' : ''}`;
		}
		return '';
	}
</script>

<li class="row" id={service.id} class:highlighted={isHighlighted}>
	<div class="head">
		<h2>{service.name}</h2>
		<StatusPill status={service.status} />
	</div>
	<p class="meta">{service.description}</p>
	<!-- One sentence per row, referenced by every refused button: the same reason nine times over
	     would be noise on the page and pain in a screen reader. -->
	<span class="visually-hidden" id={whyID}>{baseReason}</span>

	{#if isArmed}
		<!-- The only place the page raises its voice. Announced, because the button changed meaning. -->
		<div class="armed" aria-live="polite">
			<p class="warning">{armedWarning(service, armedAction)}</p>
			<div class="actions">
				<ActionButton label="cancel" reason="" describedBy={whyID} onclick={() => onCancel?.()} />
				<ActionButton
					label={`${armedAction} anyway`}
					reason=""
					destructive
					describedBy={whyID}
					onclick={() => onTap?.(armedAction)}
				/>
			</div>
		</div>
	{:else}
		<div class="actions">
			{#each actions as action (action.action)}
				<ActionButton
					label={action.label}
					reason={action.reason}
					detail={action.detail}
					busy={busyLabel(action.action)}
					destructive={action.action === 'stop'}
					describedBy={whyID}
					onclick={() => onTap?.(action.action)}
				/>
			{/each}
		</div>
	{/if}

	{#if pending}
		<p class="state" class:unclaimed={pending.unclaimed}>
			{pending.unclaimed ? `${pending.action} pending (unclaimed)` : `${pending.action} queued`}
		</p>
	{:else if finished && !finished.in_progress}
		<p class="state" class:failed={!finished.ok}>{resultLine(finished)}</p>
	{/if}
</li>

<style>
	.row {
		padding: 16px;
		border-top: 1px solid var(--color-border);
		scroll-margin-top: 72px;
	}

	.row:first-child {
		border-top: 0;
	}

	/* The restart link on a down tile arrives here as #<id> (decision #73). */
	.row.highlighted {
		background: var(--color-accent-soft);
	}

	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}

	h2 {
		font-size: 16px;
		font-weight: 500;
	}

	.meta {
		margin-top: 4px;
		font-size: 13px;
		color: var(--color-text-dim);
	}

	.actions {
		display: flex;
		flex-wrap: wrap;
		align-items: stretch;
		gap: 8px;
		margin-top: 14px;
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
		color: var(--color-text);
	}

	/* Cancel is the wider, primary button once something is armed. */
	.armed .actions :global(.button:first-child) {
		flex: 2 1 120px;
		background: var(--color-accent);
		border-color: var(--color-accent);
		color: var(--color-on-accent);
		font-weight: 500;
	}

	.state {
		margin-top: 10px;
		font-size: 12px;
		color: var(--color-text-dim);
	}

	.state.unclaimed {
		color: var(--status-degraded);
	}

	.state.failed {
		color: var(--status-down);
	}
</style>
