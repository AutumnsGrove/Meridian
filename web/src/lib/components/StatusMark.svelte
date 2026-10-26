<script lang="ts">
	import type { ServiceStatus } from '#lib/api/types';
	import { statusWord } from '#lib/state/status';

	// Shape as well as colour, because colour alone is not a signal (docs/DESIGN.md "Launcher").
	const MARKS: Record<ServiceStatus, string> = {
		ok: '●',
		degraded: '◐',
		down: '✕',
		unknown: '○',
		disabled: '—'
	};

	let { status }: { status: ServiceStatus } = $props();
</script>

<span class="mark mark-{status}" aria-hidden="true">{MARKS[status]}</span>
<span class="visually-hidden">{statusWord(status)}</span>

<style>
	.mark {
		font-size: 15px;
		line-height: 1;
		color: var(--color-text-dim);
	}

	.mark-ok {
		color: var(--status-ok);
	}

	.mark-degraded {
		color: var(--status-degraded);
	}

	.mark-down {
		color: var(--status-down);
	}

	.mark-unknown {
		color: var(--status-unknown);
	}

	.mark-disabled {
		color: var(--color-text-faint);
	}
</style>
