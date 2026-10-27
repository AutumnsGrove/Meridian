import type { AutoRestore, ControlResult, ControlStatus, PendingRequest } from '#lib/api/control';
import type { ArmState } from './armed';
import type { ControlAvailability, Service, Status } from '#lib/api/types';

/** One button on a control row. */
export type RowAction = {
	/** The action name the config and the allowlist share. */
	action: string;
	/** What the button says. Actions are verbs, so the name is the label. */
	label: string;
	/** Why the control plane as a whole cannot run it, empty when it can. */
	reason: string;
	/** A reason that belongs to this action alone (an update with no scripted updater). */
	detail: string;
};

/** The services the control page lists: the ones with a control block, in config order. */
export function controlRows(status: Status): Service[] {
	return status.sections
		.flatMap((section) => section.services)
		.filter((service) => service.control !== null);
}

/** The sentence explaining why no action works on this box, right now. */
export function controlReason(control: ControlAvailability): string {
	return control.reason !== '' ? control.reason : 'control plane not installed';
}

/** The note the page shows above the rows, or null when the control plane exists. */
export function controlNote(control: ControlAvailability): string | null {
	return control.available ? null : 'Control plane not installed on this box';
}

/**
 * The buttons for one row. Nothing here decides to enable anything: a reason on the button is the
 * whole rule, so Phase 1 shows every action and says why it is dead (docs/DESIGN.md "Control panel").
 */
export function rowActions(service: Service, control: ControlAvailability): RowAction[] {
	const reason = control.available ? '' : controlReason(control);
	return (service.control?.actions ?? []).map((action) => ({
		action,
		label: action,
		reason,
		detail: action === 'update' ? (service.control?.update_disabled_reason ?? '') : ''
	}));
}

// The slug grammar the config uses for ids (docs/STANDARDS.md section 4). Anything else in a hash is
// not a row, so it is ignored rather than looked up.
const SLUG = /^[a-z0-9][a-z0-9-]{0,31}$/;

/** The row id a URL fragment points at, or null when the fragment is not a row id. */
export function targetID(hash: string): string | null {
	const id = hash.startsWith('#') ? hash.slice(1) : hash;
	return SLUG.test(id) ? id : null;
}

/** How often the control view is asked for, once per interval (docs/agent/06-control.md T30). */
export const CONTROL_FOLLOW_UP_MS = 1_000;
export const CONTROL_IDLE_MS = 10_000;

/**
 * The page follows a queued action closely and idles otherwise. Nothing waiting means nothing to
 * watch, and a 10s idle poll is cheap on a 5W box.
 */
export function controlDelay(pendingCount: number): number {
	return pendingCount > 0 ? CONTROL_FOLLOW_UP_MS : CONTROL_IDLE_MS;
}

/** The queued request for a target, if any. */
export function pendingFor(control: ControlStatus, target: string): PendingRequest | null {
	return control.pending.find((request) => request.target === target) ?? null;
}

/** The newest result for a target, which is what a row reports after its action lands. */
export function resultFor(control: ControlStatus, target: string): ControlResult | null {
	return control.results.find((result) => result.target === target) ?? null;
}

/** The result with this id, which is how a row finds its own answer after a reconnect. */
export function resultByID(control: ControlStatus, id: string | null): ControlResult | null {
	return id === null ? null : (control.results.find((result) => result.id === id) ?? null);
}

/**
 * One action at a time, globally: while anything is queued or in progress, every other button is
 * refused with a reason rather than hidden (docs/DESIGN.md "Control panel").
 */
export function busyReason(control: ControlStatus, ownTarget: string): string {
	const busy = control.pending.find((request) => request.target !== ownTarget);
	if (busy !== undefined) {
		return `Waiting for ${busy.target} to finish ${busy.action}.`;
	}
	if (control.results.some((result) => result.in_progress)) {
		return 'Waiting for an action that is still running.';
	}
	return '';
}

/** Seconds left on an auto-restore, from the browser's clock (decision #76). Never negative. */
export function remainingSeconds(restore: AutoRestore, nowMs: number): number {
	const restoreAt = Date.parse(restore.restore_at);
	if (Number.isNaN(restoreAt)) return 0;
	return Math.max(0, Math.round((restoreAt - nowMs) / 1000));
}

/** A countdown as a person reads it: "9:59", or "0:00" when it is due. */
export function countdownLabel(seconds: number): string {
	const safe = Math.max(0, seconds);
	return `${Math.floor(safe / 60)}:${String(safe % 60).padStart(2, '0')}`;
}

/** One audit line as the history list renders it. */
export type HistoryRow = {
	id: string;
	ok: boolean;
	/** The clock time of the action, in the browser's zone. */
	time: string;
	/** "restart gitea" */
	text: string;
	/** "4.8s" */
	duration: string;
	/** The executor's sentence, or the tail of its output, for a failed row. */
	detail: string;
};

/** The action history: the audit lines the container sent, newest first, shaped for the list. */
export function historyRows(control: ControlStatus): HistoryRow[] {
	return control.audit.map((entry) => ({
		id: entry.id,
		ok: entry.ok,
		time: clockLabel(entry.at),
		text: `${entry.action} ${entry.target}`,
		duration: `${(entry.duration_ms / 1000).toFixed(1)}s`,
		detail: entry.sentence !== '' ? entry.sentence : entry.output
	}));
}

/** A timestamp as the browser's clock reads it, e.g. "18:04". */
export function clockLabel(at: string): string {
	const when = Date.parse(at);
	if (Number.isNaN(when)) return '--:--';
	return new Date(when).toLocaleTimeString(undefined, {
		hour: '2-digit',
		minute: '2-digit',
		hour12: false
	});
}

// What a button says while its action is in flight, and once it is done. The words are a table so a
// new action cannot produce "stoping".
const PROGRESS: Record<string, string> = {
	start: 'starting',
	stop: 'stopping',
	restart: 'restarting',
	update: 'updating',
	reboot: 'rebooting'
};

const PAST: Record<string, string> = {
	start: 'started',
	stop: 'stopped',
	restart: 'restarted',
	update: 'updated',
	reboot: 'rebooted'
};

/** The label a button wears while the executor is working on it. */
export function progressLabel(action: string): string {
	return `${PROGRESS[action] ?? action}…`;
}

/** The label a row reports after a successful action, e.g. "restarted in 4.8s". */
export function doneLabel(result: ControlResult): string {
	const verb = PAST[result.action] ?? result.action;
	return `${verb} in ${(result.duration_ms / 1000).toFixed(1)}s`;
}

/**
 * The sentence an armed button states, which is the whole reason the second tap exists
 * (docs/DESIGN.md "Control panel"). It says what the consequence is, not what the button does.
 */
export function armedWarning(service: Service, action: string): string {
	if (action === 'stop' && service.control?.dns === true) {
		return 'Stopping this breaks DNS for the whole tailnet, including this page. It comes back in 10 minutes unless you cancel.';
	}
	if (action === 'stop') {
		return `Stopping ${service.name} takes it offline until you start it again.`;
	}
	return `${service.name} does this right away.`;
}

/** The one-line report under a row after its action finished. */
export function resultLine(result: ControlResult): string {
	if (result.in_progress) return 'In progress.';
	if (result.ok) return doneLabel(result);
	return result.sentence !== '' ? result.sentence : 'The action failed.';
}

/**
 * The acknowledgement the executor requires for an action (decision #16). The UI guard is a courtesy
 * and this is the token it sends; the executor checks it again, which is the rule.
 */
export function ackFor(action: string, dns: boolean): string {
	if (action === 'reboot') return 'reboot-confirmed';
	if (action === 'stop') return dns ? 'dns-stop-confirmed' : 'stop-confirmed';
	return '';
}

/** True while a reboot is queued or running, which is when the page watches /healthz (T30). */
export function isRebooting(arm: ArmState, view: ControlStatus | null): boolean {
	if (arm.phase !== 'sending' && arm.phase !== 'pending') return false;
	if (arm.action !== 'reboot') return false;
	// A reboot that is still only `sending` has no id yet, so there is nothing to look up: it is
	// rebooting until the page hears otherwise.
	if (arm.phase === 'sending') return true;
	const result = view === null ? null : resultByID(view, arm.id);
	return result === null || result.in_progress;
}

/** The service name for a target slug, for a banner that has only the allowlist's name. */
export function nameForTarget(services: Service[], target: string): string {
	return services.find((service) => service.control?.target === target)?.name ?? target;
}
