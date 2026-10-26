<script lang="ts">
	import { onMount } from 'svelte';
	import { fetchStatus } from '#lib/api/client';
	import Banner from '#lib/components/Banner.svelte';
	import SearchBox from '#lib/components/SearchBox.svelte';
	import Section from '#lib/components/Section.svelte';
	import { initialPollState, startPolling, type PollState } from '#lib/state/poller';
	import { bannerText, summaryLabel } from '#lib/state/status';

	let poll: PollState = $state(initialPollState());
	let now = $state(Date.now());

	onMount(() =>
		startPolling({
			fetchStatus,
			onChange: (state) => {
				poll = state;
				now = Date.now();
			}
		})
	);

	const status = $derived(poll.status);
	const unreachable = $derived(bannerText(poll, now));
	const configRejected = $derived(
		status !== null && !status.config.ok
			? `The last config change was rejected, so the previous one is still serving. ${status.config.error}`
			: null
	);
</script>

<svelte:head>
	<title>Meridian</title>
	<meta name="description" content="Every service on this box, and whether it is up." />
</svelte:head>

<main class="shell">
	<header class="top">
		<span class="wordmark">Meridian</span>
		{#if status}
			<span class="summary">{summaryLabel(status)}</span>
		{/if}
	</header>

	{#if unreachable}
		<Banner text={unreachable} />
	{/if}
	{#if configRejected}
		<Banner text={configRejected} tone="warn" />
	{/if}

	{#if status}
		{#if status.search.buttons.length > 0}
			<SearchBox buttons={status.search.buttons} />
		{/if}

		{#each status.sections as section (section.id)}
			<Section {section} {now} />
		{/each}
	{/if}

	<a class="control-link touch-target" href="/control">
		<span>Control panel</span>
		<span aria-hidden="true">→</span>
	</a>
</main>

<style>
	.shell {
		display: flex;
		flex-direction: column;
		gap: 16px;
		max-width: 1200px;
		margin: 0 auto;
		padding: 16px;
	}

	.top {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 12px;
	}

	.wordmark {
		font-size: 18px;
		font-weight: 500;
	}

	.summary {
		font-size: var(--fs-meta);
		color: var(--color-text-dim);
		font-variant-numeric: tabular-nums;
	}

	.control-link {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0 16px;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		color: var(--color-text);
	}
</style>
