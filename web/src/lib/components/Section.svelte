<script lang="ts">
	import type { Section } from '#lib/api/types';
	import Tile from './Tile.svelte';

	let { section, now }: { section: Section; now: number } = $props();
</script>

<!-- An empty section renders nothing at all, not a heading over blank space (docs/DESIGN.md). -->
{#if section.services.length > 0}
	<section class="section" aria-labelledby="section-{section.id}">
		<h2 class="title" id="section-{section.id}">{section.title}</h2>
		<ul class="tiles">
			{#each section.services as service (service.id)}
				<li class="cell">
					<Tile {service} {now} />
				</li>
			{/each}
		</ul>
	</section>
{/if}

<style>
	.section {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.title {
		font-size: var(--fs-meta);
		font-weight: 500;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--color-text-dim);
	}

	.tiles {
		display: grid;
		grid-template-columns: 1fr;
		gap: 8px;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.cell {
		display: flex;
	}

	.cell :global(.tile) {
		flex: 1;
	}

	@media (min-width: 768px) {
		.tiles {
			grid-template-columns: repeat(2, 1fr);
		}
	}

	@media (min-width: 1200px) {
		.tiles {
			grid-template-columns: repeat(3, 1fr);
		}
	}
</style>
