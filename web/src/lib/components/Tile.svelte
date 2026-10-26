<script lang="ts">
	import type { Service } from '#lib/api/types';
	import { restartHref, tileMeta, vitalText, vitalTone } from '#lib/state/status';
	import StatusMark from './StatusMark.svelte';

	let { service, now }: { service: Service; now: number } = $props();

	// One interactive target per tile. A down service that can be restarted offers its control row
	// instead of a page that cannot answer; the restart affordance is that same link, never a
	// second target nested inside the first (docs/DESIGN.md "Launcher").
	const controlHref = $derived(restartHref(service));
	const href = $derived(controlHref ?? (service.url === '' ? null : service.url));
</script>

{#snippet body()}
	<span class="head">
		<span class="name">{service.name}</span>
		<StatusMark status={service.status} />
	</span>
	{#if service.vitals.length > 0}
		<span class="vitals">
			{#each service.vitals as vital (vital.label)}
				<span class="vital vital-{vitalTone(vital, now)}">
					<span class="visually-hidden">{vital.label}</span>
					{vitalText(vital, now)}
				</span>
			{/each}
		</span>
	{:else}
		<span class="meta">{tileMeta(service)}</span>
	{/if}
	{#if controlHref}
		<span class="restart">restart →</span>
	{/if}
{/snippet}

<!-- The tile's accessible name is its content (docs/DESIGN.md "Launcher"): the vital labels and the
     status word are hidden from sight, not from a screen reader, so there is nothing to keep in sync
     with an aria-label (decision #78). -->
{#if href}
	<a class="tile" {href} rel="noopener noreferrer">{@render body()}</a>
{:else}
	<div class="tile">{@render body()}</div>
{/if}

<style>
	.tile {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-height: 76px;
		padding: 14px 16px;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
	}

	a.tile:hover {
		border-color: var(--color-border-strong);
	}

	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}

	.name {
		font-size: var(--fs-name);
		font-weight: 500;
		color: var(--color-text);
	}

	.meta,
	.vitals {
		font-size: var(--fs-meta);
		color: var(--color-text-dim);
	}

	.vitals {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 8px;
	}

	.vital {
		font-variant-numeric: tabular-nums;
	}

	/* Old or failing readings are kept and dimmed, never hidden (docs/DESIGN.md "Launcher"). */
	.vital-stale,
	.vital-failed {
		color: var(--color-text-faint);
	}

	.restart {
		font-size: var(--fs-meta);
		color: var(--color-accent);
	}
</style>
