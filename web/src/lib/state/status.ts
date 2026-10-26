import { ageLabel } from '#lib/format/age';
import type { Service, ServiceStatus, Status, Vital } from '#lib/api/types';
import type { PollState } from './poller';

const STATUS_WORDS: Record<ServiceStatus, string> = {
	ok: 'healthy',
	degraded: 'degraded',
	down: 'down',
	unknown: 'unknown',
	disabled: 'disabled'
};

/** A vital this old is shown faint with its age, never hidden (docs/DESIGN.md "Launcher"). */
export const STALE_VITAL_S = 300;

/** The word a status is spoken as, used by the tile label and the status mark's hidden text. */
export function statusWord(status: ServiceStatus): string {
	return STATUS_WORDS[status];
}

/** The one-line summary beside the wordmark. */
export function summaryLabel(status: Status): string {
	return `${status.summary.up}/${status.summary.total} up`;
}

/**
 * Where a tile's single link goes. A down service that can be restarted offers a way into its
 * control row instead of a link to a page that cannot answer (docs/DESIGN.md "Launcher").
 */
export function restartHref(service: Service): string | null {
	if (service.status !== 'down') return null;
	if (!service.control?.actions.includes('restart')) return null;
	return `/control#${service.id}`;
}

export type VitalTone = 'normal' | 'stale' | 'failed';

/** How a vital should be drawn: failing first, then simply old. */
export function vitalTone(vital: Vital): VitalTone {
	if (vital.failing) return 'failed';
	if (vital.age_s > STALE_VITAL_S) return 'stale';
	return 'normal';
}

/** A vital's value with its age once it is old enough to say: "3 online · 2m ago". */
export function vitalText(vital: Vital, now: number): string {
	const age = ageLabel(vital.at, now);
	return age === 'now' ? vital.value : `${vital.value} · ${age} ago`;
}

/**
 * The tile's accessible name: everything the tile shows, in reading order. The visible marks are
 * colour and shape, so this sentence is what a screen reader gets.
 */
export function tileLabel(service: Service, now: number): string {
	const parts = [service.name, statusWord(service.status)];
	if (service.reason !== '') parts.push(service.reason);
	if (service.vitals.length > 0) {
		for (const vital of service.vitals) parts.push(`${vital.label} ${vitalText(vital, now)}`);
	} else if (service.reason === '' && service.description !== '') {
		parts.push(service.description);
	}
	return parts.join(', ');
}

/** The subtitle a tile shows when it has no vital to show: why it is down, or what it is. */
export function tileMeta(service: Service): string {
	return service.reason !== '' ? service.reason : service.description;
}

/** What to say when the last request failed, or null when it did not. */
export function bannerText(state: PollState, now: number): string | null {
	if (state.error === null) return null;
	if (state.lastGoodAt === null) return "Can't reach Meridian";
	return `Can't reach Meridian, showing data from ${ageLabel(state.lastGoodAt, now)} ago`;
}
