// The /api/status contract as the browser sees it. Every field is
// already formatted by Go: the browser only computes ages and search URLs (decision #59).

/** The five-value closed set a service's health can be in. */
export const SERVICE_STATUSES = ['ok', 'degraded', 'down', 'unknown', 'disabled'] as const;

export type ServiceStatus = (typeof SERVICE_STATUSES)[number];

/** A Lucide icon name, or a same-origin image path served from the config's icons directory. */
export type Icon = { lucide: string } | { image: string };

export type Vital = {
	label: string;
	value: string;
	at: string;
	age_s: number;
	failing: boolean;
};

/** What the control plane can offer for a service. Null on a read-only tile. */
export type ControlTarget = {
	target: string;
	actions: string[];
	update_disabled_reason: string;
	dns: boolean;
};

export type Service = {
	id: string;
	name: string;
	description: string;
	icon: Icon;
	url: string;
	status: ServiceStatus;
	reason: string;
	checked_at: string;
	vitals: Vital[];
	control: ControlTarget | null;
};

export type Section = {
	id: string;
	title: string;
	services: Service[];
};

export type SearchButton = {
	label: string;
	url_template: string;
};

export type HostReading = {
	at: string;
	age_s: number;
	stale: boolean;
};

/** Whether the config currently being served was accepted at load time. */
export type ConfigState = {
	ok: boolean;
	loaded_at: string;
	error: string;
};

/** Whether the control plane exists on this box at all (false when the control mounts are absent). */
export type ControlAvailability = {
	available: boolean;
	reason: string;
};

export type Summary = {
	up: number;
	total: number;
};

export type Status = {
	generated_at: string;
	config: ConfigState;
	summary: Summary;
	search: { buttons: SearchButton[] };
	control: ControlAvailability;
	host: HostReading | null;
	sections: Section[];
};
