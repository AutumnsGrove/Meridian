/**
 * A short age label for a timestamp: "now", "4m", "3h", "5d". Callers append " ago" when the
 * sentence needs it. `at` is an ISO string from the API or epoch milliseconds from the local clock
 * (the last good response). The API sends ages in seconds; this is for the two things the browser
 * ages itself.
 *
 * A timestamp in the future, or one that does not parse, reads as "now": a clock skew of a few
 * seconds must not render as a negative age, and a broken timestamp must not render as NaN.
 */
export function ageLabel(at: string | number, now: number): string {
	const then = typeof at === 'number' ? at : Date.parse(at);
	if (Number.isNaN(then)) return 'now';

	const seconds = Math.floor((now - then) / 1000);
	if (seconds < 45) return 'now';

	const minutes = Math.max(1, Math.floor(seconds / 60));
	if (minutes < 60) return `${minutes}m`;

	const hours = Math.floor(minutes / 60);
	if (hours < 24) return `${hours}h`;

	return `${Math.floor(hours / 24)}d`;
}
