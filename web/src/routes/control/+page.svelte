<script lang="ts">
	import { onMount } from 'svelte';
	import Banner from '#lib/components/Banner.svelte';
	import RebootCard from '#lib/components/RebootCard.svelte';
	import ServiceRow from '#lib/components/ServiceRow.svelte';
	import { controlNote, controlReason, controlRows, targetID } from '#lib/state/control';
	import { createLiveStatus } from '#lib/state/live.svelte';

	const live = createLiveStatus();
	onMount(live.start);

	let fragment = $state(window.location.hash);
	let highlighted = $state<string | null>(null);

	onMount(() => {
		const onHashChange = () => (fragment = window.location.hash);
		window.addEventListener('hashchange', onHashChange);
		return () => window.removeEventListener('hashchange', onHashChange);
	});

	// A down tile sends the launcher here with #<id>, but the row does not exist until the first
	// response arrives, so the browser's own jump finds nothing. Do it when the data lands.
	$effect(() => {
		const id = live.poll.status === null ? null : targetID(fragment);
		const element = id === null ? null : document.getElementById(id);
		highlighted = element === null ? null : id;
		if (element !== null) element.scrollIntoView({ block: 'start' });
	});

	const status = $derived(live.poll.status);
	const note = $derived(status === null ? null : controlNote(status.control));
	const rows = $derived(status === null ? [] : controlRows(status));
	const rebootReason = $derived(
		status === null
			? ''
			: status.control.available
				? 'Reboot is not wired up yet'
				: controlReason(status.control)
	);
</script>

<svelte:head>
	<title>Control · Meridian</title>
	<meta name="description" content="Restart, update or stop a service on this box." />
</svelte:head>

<header class="bar">
	<a class="back" href="/"><span aria-hidden="true">←</span><span>Meridian</span></a>
	<span class="crumb" aria-hidden="true">/</span>
	<h1>Control</h1>
</header>

<main class="page">
	{#if note}
		<Banner text={note} tone="warn" />
	{/if}

	{#if status}
		<ul class="rows" aria-label="Services">
			{#each rows as service (service.id)}
				<ServiceRow {service} control={status.control} isHighlighted={highlighted === service.id} />
			{/each}
		</ul>

		<RebootCard reason={rebootReason} />
	{/if}
</main>

<style>
	.bar {
		position: sticky;
		top: 0;
		z-index: 5;
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 56px;
		padding: 0 8px;
		background: color-mix(in srgb, var(--color-bg) 92%, transparent);
		backdrop-filter: blur(8px);
		border-bottom: 1px solid var(--color-border);
	}

	.back {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		min-height: var(--touch);
		padding: 0 10px;
		border-radius: 8px;
		font-size: 14px;
		color: var(--color-text-dim);
	}

	.back:hover {
		color: var(--color-text);
		background: var(--color-surface);
	}

	.crumb {
		margin-left: -4px;
		color: var(--color-border-strong);
	}

	h1 {
		font-size: 16px;
		font-weight: 600;
	}

	.page {
		display: flex;
		flex-direction: column;
		gap: 20px;
		max-width: 1120px;
		margin: 0 auto;
		padding: 16px 16px 36px;
	}

	.rows {
		margin: 0;
		padding: 0;
		list-style: none;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: 12px;
		overflow: hidden;
	}

	@media (min-width: 768px) {
		.bar {
			padding: 0 24px;
		}

		.page {
			padding: 28px 32px 56px;
		}
	}
</style>
