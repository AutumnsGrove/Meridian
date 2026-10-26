<script lang="ts">
	import type { ControlAvailability, Service } from '#lib/api/types';
	import { controlReason, rowActions } from '#lib/state/control';
	import ActionButton from './ActionButton.svelte';
	import StatusPill from './StatusPill.svelte';

	let {
		service,
		control,
		isHighlighted = false
	}: { service: Service; control: ControlAvailability; isHighlighted?: boolean } = $props();

	const actions = $derived(rowActions(service, control));
	const whyID = $derived(`why-${service.id}`);
	const baseReason = $derived(control.available ? '' : controlReason(control));
</script>

<li class="row" id={service.id} class:highlighted={isHighlighted}>
	<div class="head">
		<h3>{service.name}</h3>
		<StatusPill status={service.status} />
	</div>
	<p class="meta">{service.description}</p>
	<!-- One sentence per row, referenced by every refused button: the same reason nine times over
	     would be noise on the page and pain in a screen reader. -->
	<span class="visually-hidden" id={whyID}>{baseReason}</span>
	<div class="actions">
		{#each actions as action (action.action)}
			<ActionButton
				label={action.label}
				reason={action.reason}
				detail={action.detail}
				describedBy={whyID}
			/>
		{/each}
	</div>
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

	h3 {
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
</style>
