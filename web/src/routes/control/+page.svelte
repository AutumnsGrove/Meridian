<script lang="ts">
	import { onMount } from 'svelte';
	import { postControl } from '#lib/api/client';
	import ActionHistory from '#lib/components/ActionHistory.svelte';
	import Banner from '#lib/components/Banner.svelte';
	import RebootCard from '#lib/components/RebootCard.svelte';
	import RestoreBanner from '#lib/components/RestoreBanner.svelte';
	import ServiceRow from '#lib/components/ServiceRow.svelte';
	import { initialArmState, reduceArm, type ArmState } from '#lib/state/armed';
	import {
		ackFor,
		controlNote,
		controlReason,
		controlRows,
		historyRows,
		isRebooting,
		nameForTarget,
		resultByID,
		targetID
	} from '#lib/state/control';
	import { createLiveControl } from '#lib/state/controlLive.svelte';
	import { createLiveStatus } from '#lib/state/live.svelte';
	import { watchForRecovery } from '#lib/state/reconnect';

	const live = createLiveStatus();
	const sync = createLiveControl();
	onMount(live.start);

	// One armed-button machine for the whole page: two actions at once is what this page exists to
	// prevent, so the second tap only ever belongs to the row that armed it.
	let arm = $state<ArmState>(initialArmState());
	let activeTarget = $state<string | null>(null);

	const status = $derived(live.poll.status);
	const view = $derived(sync.state.value);
	const rows = $derived(status === null ? [] : controlRows(status));
	const note = $derived(status === null ? null : controlNote(status.control));
	const unavailable = $derived(status !== null && !status.control.available);
	const history = $derived(view === null ? [] : historyRows(view));
	const rebooting = $derived(isRebooting(arm, view));

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
		const id = status === null ? null : targetID(fragment);
		const element = id === null ? null : document.getElementById(id);
		highlighted = element === null ? null : id;
		if (element !== null) element.scrollIntoView({ block: 'start' });
	});

	// Only poll the control view when there is one. A box without an executor answers 503 to that
	// route, and the
	// browser logs a failed request as a console error, so asking a question nobody can answer would
	// put an error on a page that is working exactly as intended.
	$effect(() => {
		if (status === null || !status.control.available) return;
		return sync.start();
	});

	// The arm expires on its own: the second tap is only meaningful inside the five seconds the copy
	// promised. Re-arming starts the clock again.
	$effect(() => {
		if (arm.phase !== 'armed') return;
		const remaining = Math.max(0, arm.expiresAt - Date.now());
		const timer = setTimeout(() => {
			arm = reduceArm(arm, { type: 'expire', now: Date.now() }).state;
		}, remaining);
		return () => clearTimeout(timer);
	});

	// A queued request is pending until its result arrives. The container answers the poll, so the row
	// learns the outcome the same way it would after a reload.
	$effect(() => {
		if (arm.phase !== 'pending' || view === null) return;
		const result = resultByID(view, arm.id);
		if (result === null || result.in_progress) return;
		arm = reduceArm(arm, {
			type: 'result',
			id: result.id,
			ok: result.ok,
			sentence: result.sentence
		}).state;
	});

	// A reboot or a Meridian restart takes the hub away: wait for /healthz to answer, then ask for the
	// finished result. Everything else is already on the page.
	$effect(() => {
		if (!rebooting) return;
		return watchForRecovery({
			probe: async () => {
				try {
					const response = await fetch('/healthz', { cache: 'no-store', redirect: 'error' });
					return response.ok;
				} catch {
					return false;
				}
			},
			onAttempt: () => {},
			onRecovered: () => sync.refresh()
		});
	});

	function tap(action: string, target: string): void {
		const outcome = reduceArm(arm, { type: 'tap', action, now: Date.now() });
		arm = outcome.state;
		activeTarget = target;
		if (outcome.effect.type === 'send') void send(outcome.effect.action, target);
	}

	function cancel(): void {
		arm = reduceArm(arm, { type: 'cancel' }).state;
		activeTarget = null;
	}

	async function send(action: string, target: string): Promise<void> {
		const service = rows.find((row) => (row.control?.target ?? row.id) === target);
		const outcome = await postControl({
			action,
			target,
			ack: ackFor(action, service?.control?.dns ?? false)
		});
		arm = outcome.ok
			? reduceArm(arm, { type: 'sent', id: outcome.id }).state
			: reduceArm(arm, { type: 'refused', sentence: outcome.sentence }).state;
		sync.refresh();
	}

	function cancelRestore(ref: string, target: string): void {
		void postControl({ action: 'cancel-autorestore', target, ack: '', ref }).then(() =>
			sync.refresh()
		);
	}
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

	{#if view !== null && view.executor.state === 'missing'}
		<Banner
			text="No executor. Actions are queued but not being picked up."
			detail="The trigger unit is not running. Requests waiting more than two minutes are refused as stale when it returns."
			tone="warn"
		/>
	{/if}

	{#if rebooting}
		<Banner
			text="Rebooting. Back in about 40s."
			detail="This page reconnects on its own when the hub answers."
			tone="warn"
		/>
	{/if}

	{#each view?.autorestore ?? [] as restore (restore.ref)}
		<RestoreBanner
			{restore}
			name={nameForTarget(rows, restore.target)}
			now={sync.now}
			onCancel={(ref) => cancelRestore(ref, restore.target)}
		/>
	{/each}

	{#if status}
		<ul class="rows" aria-label="Services">
			{#each rows as service (service.id)}
				<ServiceRow
					{service}
					control={status.control}
					{view}
					{arm}
					{activeTarget}
					isHighlighted={highlighted === service.id}
					onTap={(action) => tap(action, service.control?.target ?? service.id)}
					onCancel={cancel}
				/>
			{/each}
		</ul>

		<RebootCard
			reason={unavailable ? controlReason(status.control) : ''}
			{arm}
			isActive={activeTarget === 'host'}
			onTap={(action) => tap(action, 'host')}
			onCancel={cancel}
		/>

		<ActionHistory rows={history} />
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
