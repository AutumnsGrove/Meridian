import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { parseStatus } from '#lib/api/parse';
import type { Service, Status } from '#lib/api/types';

// The golden files are the contract shared with the Go tests (decision #59), and the portable
// fixture inside them is all a component test needs: no service here exists on the real box.
// Vitest runs with web/ as the working directory.
const goldenDir = resolve(process.cwd(), '../testdata/api');

/** Every golden response, in file-name order. */
export function goldenNames(): string[] {
	return readdirSync(goldenDir)
		.filter((name) => name.endsWith('.golden.json'))
		.sort();
}

/** One golden file, parsed through the same validator the app uses. */
export function loadStatus(name: string): Status {
	const raw: unknown = JSON.parse(readFileSync(resolve(goldenDir, name), 'utf8'));
	const result = parseStatus(raw);
	if (!result.ok) throw new Error(`${name} is not a valid status: ${result.error}`);
	return result.value;
}

/** The first service with this name across all sections. */
export function serviceNamed(status: Status, name: string): Service {
	for (const section of status.sections) {
		const match = section.services.find((service) => service.name === name);
		if (match) return match;
	}
	throw new Error(`${name} is not in ${status.generated_at}'s sections`);
}
