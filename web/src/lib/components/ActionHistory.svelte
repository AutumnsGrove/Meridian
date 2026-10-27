<script lang="ts">
	import type { HistoryRow } from '#lib/state/control';

	let { rows }: { rows: HistoryRow[] } = $props();
</script>

<section class="history" aria-labelledby="history-title">
	<header>
		<h2 id="history-title">Action history</h2>
		<span class="count">Last {rows.length}</span>
	</header>

	{#if rows.length === 0}
		<p class="empty">Nothing has been pressed yet.</p>
	{:else}
		<ul>
			{#each rows as row (row.id)}
				<li class:failed={!row.ok}>
					{#if row.ok}
						<p class="line">
							<time datetime={row.time}>{row.time}</time>
							<span class="what">{row.text}</span>
							<span class="result">{row.duration} · ok</span>
						</p>
					{:else}
						<!-- A failed row opens to show the executor's own words: the receipt is the point. -->
						<details>
							<summary class="line">
								<time datetime={row.time}>{row.time}</time>
								<span class="what">{row.text}</span>
								<span class="result">{row.duration} · failed</span>
							</summary>
							<p class="detail">{row.detail}</p>
						</details>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.history {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding-top: 28px;
		border-top: 1px solid var(--color-border);
	}

	header {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 12px;
	}

	h2 {
		font-size: var(--fs-meta);
		font-weight: 500;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--color-text-dim);
	}

	.count,
	.empty {
		font-size: var(--fs-meta);
		color: var(--color-text-faint);
	}

	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius);
		overflow: hidden;
	}

	li {
		border-top: 1px solid var(--color-border);
	}

	li:first-child {
		border-top: 0;
	}

	.line {
		display: grid;
		grid-template-columns: 48px 1fr auto;
		gap: 10px;
		align-items: baseline;
		min-height: var(--touch);
		padding: 10px 14px;
		font-size: 13px;
	}

	summary {
		cursor: pointer;
		list-style: none;
	}

	summary::-webkit-details-marker {
		display: none;
	}

	time {
		font-variant-numeric: tabular-nums;
		color: var(--color-text-dim);
	}

	.what {
		color: var(--color-text);
	}

	.result {
		color: var(--color-text-dim);
		font-variant-numeric: tabular-nums;
	}

	.failed {
		border-left: 3px solid var(--status-down);
	}

	.failed .result {
		color: var(--status-down);
	}

	.detail {
		padding: 0 14px 12px 72px;
		font-family: var(--font-mono);
		font-size: 12px;
		white-space: pre-wrap;
		color: var(--color-text-dim);
		overflow-wrap: anywhere;
	}
</style>
