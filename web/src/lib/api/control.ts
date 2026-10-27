/**
 * GET /api/control/status as the browser sees it (decision #82). Types arrive raw: the page ages
 * results and counts down a restore from these times (decision #76), and every sentence is the
 * container's own, built from the closed code table (decision #59).
 */
import {
	Refusal,
	join,
	object,
	read,
	readArray,
	readBoolean,
	readInteger,
	readOptionalBoolean,
	readOptionalString,
	readString,
	type Fields,
	type ParseOutcome
} from './fields';

/** How the host side is doing, as far as the container can tell from data on disk. */
export const EXECUTOR_STATES = ['ok', 'missing', 'unknown'] as const;
export type ExecutorState = (typeof EXECUTOR_STATES)[number];

export type ExecutorView = {
	state: ExecutorState;
	/** When the trigger unit entered its state; null when the host snapshot does not say. */
	since: string | null;
};

export type PendingRequest = {
	id: string;
	action: string;
	target: string;
	requested_at: string;
	age_s: number;
	/** True when nothing has picked it up, which is what a dead executor looks like. */
	unclaimed: boolean;
};

export type ControlResult = {
	id: string;
	action: string;
	target: string;
	ok: boolean;
	code: string;
	exit_code: number;
	started_at: string;
	finished_at: string;
	duration_ms: number;
	output: string;
	note: string;
	restore_at: string | null;
	ref: string;
	in_progress: boolean;
	/** The code's sentence, empty for a success. */
	sentence: string;
};

export type AuditEntry = {
	at: string;
	id: string;
	action: string;
	target: string;
	ok: boolean;
	code: string;
	duration_ms: number;
	by: string;
	output: string;
	sentence: string;
};

export type AutoRestore = {
	/** The stop request's id, which is what a cancel names. */
	ref: string;
	target: string;
	restore_at: string;
	remaining_s: number;
};

export type ControlStatus = {
	generated_at: string;
	executor: ExecutorView;
	pending: PendingRequest[];
	results: ControlResult[];
	audit: AuditEntry[];
	autorestore: AutoRestore[];
};

function readExecutorState(fields: Fields, key: string, path: string): ExecutorState {
	const value = read(fields, key, path);
	if (typeof value !== 'string' || !(EXECUTOR_STATES as readonly string[]).includes(value)) {
		throw new Refusal(`${join(path, key)}: want one of ${EXECUTOR_STATES.join(', ')}`);
	}
	return value as ExecutorState;
}

function parseExecutor(value: unknown, path: string): ExecutorView {
	const fields = object(value, path);
	return {
		state: readExecutorState(fields, 'state', path),
		since: readOptionalString(fields, 'since', path)
	};
}

function parsePending(value: unknown, path: string): PendingRequest {
	const fields = object(value, path);
	return {
		id: readString(fields, 'id', path),
		action: readString(fields, 'action', path),
		target: readString(fields, 'target', path),
		requested_at: readString(fields, 'requested_at', path),
		age_s: readInteger(fields, 'age_s', path),
		unclaimed: readBoolean(fields, 'unclaimed', path)
	};
}

// The wire.Result fields a result carries, in one place because results and audit lines share most
// of them and the page reads both.
function parseResult(value: unknown, path: string): ControlResult {
	const fields = object(value, path);
	return {
		id: readString(fields, 'id', path),
		action: readString(fields, 'action', path),
		target: readString(fields, 'target', path),
		ok: readBoolean(fields, 'ok', path),
		code: readString(fields, 'code', path),
		exit_code: readInteger(fields, 'exit_code', path),
		started_at: readString(fields, 'started_at', path),
		finished_at: readString(fields, 'finished_at', path),
		duration_ms: readInteger(fields, 'duration_ms', path),
		output: readString(fields, 'output', path),
		note: readString(fields, 'note', path),
		restore_at: readOptionalString(fields, 'restore_at', path),
		ref: readOptionalString(fields, 'ref', path) ?? '',
		in_progress: readOptionalBoolean(fields, 'in_progress', path) ?? false,
		sentence: readString(fields, 'sentence', path)
	};
}

function parseAudit(value: unknown, path: string): AuditEntry {
	const fields = object(value, path);
	return {
		at: readString(fields, 'at', path),
		id: readString(fields, 'id', path),
		action: readString(fields, 'action', path),
		target: readString(fields, 'target', path),
		ok: readBoolean(fields, 'ok', path),
		code: readString(fields, 'code', path),
		duration_ms: readInteger(fields, 'duration_ms', path),
		by: readString(fields, 'by', path),
		output: readString(fields, 'output', path),
		sentence: readString(fields, 'sentence', path)
	};
}

function parseRestore(value: unknown, path: string): AutoRestore {
	const fields = object(value, path);
	return {
		ref: readString(fields, 'ref', path),
		target: readString(fields, 'target', path),
		restore_at: readString(fields, 'restore_at', path),
		remaining_s: readInteger(fields, 'remaining_s', path)
	};
}

function parse(value: unknown): ControlStatus {
	const fields = object(value, '');
	return {
		generated_at: readString(fields, 'generated_at', ''),
		executor: parseExecutor(read(fields, 'executor', ''), 'executor'),
		pending: readArray(fields, 'pending', '').map((item, index) =>
			parsePending(item, `pending.${index}`)
		),
		results: readArray(fields, 'results', '').map((item, index) =>
			parseResult(item, `results.${index}`)
		),
		audit: readArray(fields, 'audit', '').map((item, index) => parseAudit(item, `audit.${index}`)),
		autorestore: readArray(fields, 'autorestore', '').map((item, index) =>
			parseRestore(item, `autorestore.${index}`)
		)
	};
}

/**
 * Validates an untrusted JSON value against the control status contract. It never throws and never
 * casts; a refused value names the field that failed.
 */
export function parseControlStatus(input: unknown): ParseOutcome<ControlStatus> {
	try {
		return { ok: true, value: parse(input) };
	} catch (error) {
		if (error instanceof Refusal) return { ok: false, error: error.message };
		throw error;
	}
}
