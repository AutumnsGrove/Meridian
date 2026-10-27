import {
	SERVICE_STATUSES,
	type ConfigState,
	type ControlAvailability,
	type ControlTarget,
	type HostReading,
	type Icon,
	type SearchButton,
	type Section,
	type Service,
	type ServiceStatus,
	type Status,
	type Summary,
	type Vital
} from './types';
import {
	Refusal,
	join,
	object,
	read,
	readArray,
	readBoolean,
	readInteger,
	readString,
	readStrings,
	refuse,
	type Fields,
	type ParseOutcome
} from './fields';

/** A parsed response, or the path and reason the first bad field was refused for. */
export type ParseResult = ParseOutcome<Status>;

// readStatus is the closed service-status vocabulary, which belongs to /api/status alone.
function readStatus(fields: Fields, key: string, path: string): ServiceStatus {
	const value = read(fields, key, path);
	if (typeof value !== 'string' || !(SERVICE_STATUSES as readonly string[]).includes(value)) {
		refuse(join(path, key), `one of ${SERVICE_STATUSES.join(', ')}`);
	}
	return value as ServiceStatus;
}

function parseIcon(value: unknown, path: string): Icon {
	const fields = object(value, path);
	const hasLucide = 'lucide' in fields;
	const hasImage = 'image' in fields;
	if (hasLucide === hasImage) refuse(path, 'exactly one of lucide or image');
	return hasLucide
		? { lucide: readString(fields, 'lucide', path) }
		: { image: readString(fields, 'image', path) };
}

function parseVital(value: unknown, path: string): Vital {
	const fields = object(value, path);
	return {
		label: readString(fields, 'label', path),
		value: readString(fields, 'value', path),
		at: readString(fields, 'at', path),
		age_s: readInteger(fields, 'age_s', path),
		failing: readBoolean(fields, 'failing', path)
	};
}

function parseControl(value: unknown, path: string): ControlTarget | null {
	if (value === null) return null;
	const fields = object(value, path);
	return {
		target: readString(fields, 'target', path),
		actions: readStrings(fields, 'actions', path),
		update_disabled_reason: readString(fields, 'update_disabled_reason', path),
		dns: readBoolean(fields, 'dns', path)
	};
}

function parseService(value: unknown, path: string): Service {
	const fields = object(value, path);
	return {
		id: readString(fields, 'id', path),
		name: readString(fields, 'name', path),
		description: readString(fields, 'description', path),
		icon: parseIcon(read(fields, 'icon', path), join(path, 'icon')),
		url: readString(fields, 'url', path),
		status: readStatus(fields, 'status', path),
		reason: readString(fields, 'reason', path),
		checked_at: readString(fields, 'checked_at', path),
		vitals: readArray(fields, 'vitals', path).map((vital, index) =>
			parseVital(vital, join(join(path, 'vitals'), index))
		),
		control: parseControl(read(fields, 'control', path), join(path, 'control'))
	};
}

function parseSection(value: unknown, path: string): Section {
	const fields = object(value, path);
	return {
		id: readString(fields, 'id', path),
		title: readString(fields, 'title', path),
		services: readArray(fields, 'services', path).map((service, index) =>
			parseService(service, join(join(path, 'services'), index))
		)
	};
}

function parseSearchButton(value: unknown, path: string): SearchButton {
	const fields = object(value, path);
	return {
		label: readString(fields, 'label', path),
		url_template: readString(fields, 'url_template', path)
	};
}

function parseSearch(value: unknown, path: string): { buttons: SearchButton[] } {
	const fields = object(value, path);
	return {
		buttons: readArray(fields, 'buttons', path).map((button, index) =>
			parseSearchButton(button, join(join(path, 'buttons'), index))
		)
	};
}

function parseConfig(value: unknown, path: string): ConfigState {
	const fields = object(value, path);
	return {
		ok: readBoolean(fields, 'ok', path),
		loaded_at: readString(fields, 'loaded_at', path),
		error: readString(fields, 'error', path)
	};
}

function parseSummary(value: unknown, path: string): Summary {
	const fields = object(value, path);
	return { up: readInteger(fields, 'up', path), total: readInteger(fields, 'total', path) };
}

function parseControlAvailability(value: unknown, path: string): ControlAvailability {
	const fields = object(value, path);
	return {
		available: readBoolean(fields, 'available', path),
		reason: readString(fields, 'reason', path)
	};
}

function parseHost(value: unknown, path: string): HostReading | null {
	if (value === null) return null;
	const fields = object(value, path);
	return {
		at: readString(fields, 'at', path),
		age_s: readInteger(fields, 'age_s', path),
		stale: readBoolean(fields, 'stale', path)
	};
}

function parse(value: unknown): Status {
	const fields = object(value, '');
	return {
		generated_at: readString(fields, 'generated_at', ''),
		config: parseConfig(read(fields, 'config', ''), 'config'),
		summary: parseSummary(read(fields, 'summary', ''), 'summary'),
		search: parseSearch(read(fields, 'search', ''), 'search'),
		control: parseControlAvailability(read(fields, 'control', ''), 'control'),
		host: parseHost(read(fields, 'host', ''), 'host'),
		sections: readArray(fields, 'sections', '').map((section, index) =>
			parseSection(section, `sections.${index}`)
		)
	};
}

/**
 * Validates an untrusted JSON value against the /api/status contract. It never throws and never
 * casts: an accepted value is structurally a Status, and a refused one names the field that failed.
 */
export function parseStatus(input: unknown): ParseResult {
	try {
		return { ok: true, value: parse(input) };
	} catch (error) {
		if (error instanceof Refusal) return { ok: false, error: error.message };
		throw error;
	}
}
