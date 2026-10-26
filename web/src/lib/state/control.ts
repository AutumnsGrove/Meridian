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
