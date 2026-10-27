/**
 * The field readers every wire parser in this directory uses. They throw a Refusal rather than
 * returning an error, so a parser can read a document in one straight line and the caller turns the
 * first refusal into a message that names the field (docs/STANDARDS.md section 4).
 */

/** A validated value, or the path and expectation of the first field that failed. */
export type ParseOutcome<T> = { ok: true; value: T } | { ok: false; error: string };

export type Fields = { [key: string]: unknown };

// Refusal is control flow, not a failure: parseStatus turns it into a result. Anything else that
// throws is a bug in this file and must surface as a test failure.
export class Refusal extends Error {}

export function refuse(path: string, want: string): never {
	throw new Refusal(`${path}: want ${want}`);
}

export function join(path: string, key: string | number): string {
	return path === '' ? String(key) : `${path}.${String(key)}`;
}

export function object(value: unknown, path: string): Fields {
	if (typeof value !== 'object' || value === null || Array.isArray(value)) {
		refuse(path, 'an object');
	}
	return value as Fields;
}

export function read(fields: Fields, key: string, path: string): unknown {
	const where = join(path, key);
	if (!(key in fields)) refuse(where, 'present');
	return fields[key];
}

export function readString(fields: Fields, key: string, path: string): string {
	const value = read(fields, key, path);
	if (typeof value !== 'string') refuse(join(path, key), 'a string');
	return value;
}

export function readInteger(fields: Fields, key: string, path: string): number {
	const value = read(fields, key, path);
	if (typeof value !== 'number' || !Number.isInteger(value)) {
		refuse(join(path, key), 'an integer');
	}
	return value;
}

export function readBoolean(fields: Fields, key: string, path: string): boolean {
	const value = read(fields, key, path);
	if (typeof value !== 'boolean') refuse(join(path, key), 'a boolean');
	return value;
}

export function readArray(fields: Fields, key: string, path: string): unknown[] {
	const value = read(fields, key, path);
	if (!Array.isArray(value)) refuse(join(path, key), 'an array');
	return value;
}

export function readStrings(fields: Fields, key: string, path: string): string[] {
	return readArray(fields, key, path).map((item, index) => {
		const where = join(join(path, key), index);
		if (typeof item !== 'string') refuse(where, 'a string');
		return item;
	});
}

/** An optional string: absent and undefined both mean "the field is not there". */
export function readOptionalString(fields: Fields, key: string, path: string): string | null {
	const value = fields[key];
	if (value === undefined || value === null) return null;
	if (typeof value !== 'string') refuse(join(path, key), 'a string');
	return value;
}
