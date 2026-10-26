<script lang="ts">
	// Self-hosted so the page works with no network and makes no third-party request at all
	// (docs/STANDARDS.md section 6, decision #58).
	import '@fontsource-variable/lexend';
	import '#lib/styles/tokens.css';

	let { children } = $props();
</script>

{@render children()}
