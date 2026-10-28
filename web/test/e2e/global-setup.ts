import { spawn, spawnSync, type ChildProcess } from 'node:child_process';
import {
	createWriteStream,
	existsSync,
	mkdirSync,
	readFileSync,
	rmSync,
	writeFileSync
} from 'node:fs';
import { createServer } from 'node:net';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { statePath, type RunState } from './state';

// The suite runs the binary that ships, on the portable fixture, against a stub upstream
// (docs/agent/04-gateway-and-web.md, T19). Everything a machine decides is chosen here: two free
// ports, one temp directory, and a copy of the fixture with those three values substituted in.
// Nothing real is touched: no systemd, no Docker, no tailnet.

const webDir = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');
const repoRoot = resolve(webDir, '..');
const binary = join(repoRoot, 'dist', 'meridian');
const runDir = dirname(statePath);
const configPath = join(runDir, 'config.yaml');
const hostFile = join(runDir, 'host.json');
const controlDir = join(runDir, 'control');
const appLog = join(runDir, 'meridian.log');
const stubLog = join(runDir, 'stub.log');

/** How long a server may take to answer its first request before the run is declared broken. */
const startTimeoutMs = 20_000;

/** How long the warm-up may spend waiting for the first complete set of probe results. */
const warmTimeoutMs = 20_000;

/** A port the OS says is free, released again before the caller binds it. */
function freePort(): Promise<number> {
	return new Promise((done, fail) => {
		const probe = createServer();
		probe.once('error', fail);
		probe.listen(0, '127.0.0.1', () => {
			const address = probe.address();
			if (address === null || typeof address === 'string') {
				fail(new Error('the OS did not report a port'));
				return;
			}
			probe.close(() => done(address.port));
		});
	});
}

/** Writes the portable fixture with this run's port, stub address and host snapshot path. */
function writeConfig(appAddress: string, stubAddress: string): void {
	const fixture = readFileSync(join(repoRoot, 'testdata', 'portable', 'config.yaml'), 'utf8');
	const substitutions = [
		['listen: 127.0.0.1:8891', `listen: ${appAddress}`],
		['http://localhost:3000', `http://${stubAddress}/gitea`],
		['http://localhost:8096', `http://${stubAddress}/jellyfin`],
		['file: /status/host.json', `file: ${hostFile}`],
		// The control plane is a directory pair in the real container; here it is this run's temp dir,
		// and reboot is allowed so the page's danger zone is live.
		['inbox_dir: /control/inbox', `inbox_dir: ${join(controlDir, 'inbox')}`],
		['outbox_dir: /control/outbox', `outbox_dir: ${join(controlDir, 'outbox')}`],
		['reboot: false', 'reboot: true']
	] as const;

	let text = fixture;
	for (const [from, to] of substitutions) {
		if (!text.includes(from)) {
			throw new Error(`the portable fixture no longer contains ${from}: update the e2e setup`);
		}
		text = text.replaceAll(from, to);
	}
	writeFileSync(configPath, text);
}

/**
 * Writes the snapshot the host-side probe would write. It is refreshed while the suite runs, the way
 * the timer on the box refreshes it: a snapshot frozen at setup would go stale after two minutes and
 * quietly turn the two host-probed tiles unknown halfway through a run.
 */
function writeHostSnapshot(): void {
	const now = new Date();
	const since = new Date(now.getTime() - 86_400_000).toISOString();
	writeFileSync(
		hostFile,
		JSON.stringify(
			{
				at: now.toISOString(),
				host: { uptime_s: 1_412_033, load1: 0.31 },
				units: { 'backup.service': { active: true, since } },
				tailscale: { online: true, exit_node: true, peers_online: 2, peers_total: 3 }
			},
			null,
			2
		)
	);
}

/** The last few lines of a server's log, which is what a failed start has to explain itself with. */
function logTail(path: string): string {
	if (!existsSync(path)) return '(no log)';
	return readFileSync(path, 'utf8').trim().split('\n').slice(-10).join('\n') || '(nothing logged)';
}

/** Starts a process with its output in a log file, and resolves once it has actually started. */
function start(command: string, args: string[], log: string): Promise<ChildProcess> {
	return new Promise((done, fail) => {
		const child = spawn(command, args, { cwd: repoRoot, stdio: ['ignore', 'pipe', 'pipe'] });
		const output = createWriteStream(log);
		child.stdout?.pipe(output);
		child.stderr?.pipe(output);
		child.once('error', (error) => fail(new Error(`${command} did not start: ${error.message}`)));
		child.once('spawn', () => done(child));
	});
}

/** Waits for a URL to answer, or explains what the process said before giving up. */
async function waitFor(url: string, what: string, child: ChildProcess, log: string): Promise<void> {
	const deadline = Date.now() + startTimeoutMs;
	while (Date.now() < deadline) {
		if (child.exitCode !== null) {
			throw new Error(
				`${what} exited with ${child.exitCode} before ${url} answered:\n${logTail(log)}`
			);
		}
		try {
			const response = await fetch(url);
			if (response.ok) return;
		} catch {
			// Not listening yet; that is the normal state for the first few attempts.
		}
		await new Promise((sleep) => setTimeout(sleep, 100));
	}
	throw new Error(`${what} never answered ${url}:\n${logTail(log)}`);
}

/**
 * Waits until every service has been probed once, so the first spec sees the settled page rather
 * than a page still filling in. The fixture has four services, two of them host-probed.
 */
async function waitForFirstPoll(baseURL: string, child: ChildProcess): Promise<void> {
	const deadline = Date.now() + warmTimeoutMs;
	let summary = 'no answer yet';
	while (Date.now() < deadline) {
		if (child.exitCode !== null) {
			throw new Error(
				`meridian exited with ${child.exitCode} while warming up:\n${logTail(appLog)}`
			);
		}
		try {
			const response = await fetch(`${baseURL}/api/status`, { cache: 'no-store' });
			const status = (await response.json()) as { summary: { up: number; total: number } };
			summary = `${status.summary.up} of ${status.summary.total} up`;
			if (status.summary.total > 0 && status.summary.up === status.summary.total) return;
		} catch {
			// The gateway answered /healthz already, so this is a torn-down socket, not a dead process.
		}
		await new Promise((sleep) => setTimeout(sleep, 200));
	}
	throw new Error(
		`the fixture never came up (${summary}):\n${logTail(appLog)}\n${logTail(stubLog)}`
	);
}

/** Asks a process to stop, and takes it out of the way if it will not. */
async function stop(child: ChildProcess | null): Promise<void> {
	if (child === null || child.exitCode !== null || child.signalCode !== null) return;
	const exited = new Promise<void>((done) => child.once('exit', () => done()));
	child.kill('SIGTERM');
	const impatient = setTimeout(() => child.kill('SIGKILL'), 5000);
	await exited;
	clearTimeout(impatient);
}

export default async function globalSetup(): Promise<() => Promise<void>> {
	if (!existsSync(binary)) {
		throw new Error(`${binary} is missing: run scripts/check e2e, which builds it and the app`);
	}

	rmSync(runDir, { recursive: true, force: true });
	mkdirSync(runDir, { recursive: true });
	for (const dir of ['inbox/queue', 'inbox/tmp', 'outbox/results']) {
		mkdirSync(join(controlDir, dir), { recursive: true });
	}

	const [appPort, stubPort] = await Promise.all([freePort(), freePort()]);
	const appAddress = `127.0.0.1:${appPort}`;
	const stubAddress = `127.0.0.1:${stubPort}`;
	const baseURL = `http://${appAddress}`;
	const stubURL = `http://${stubAddress}`;

	writeHostSnapshot();
	writeConfig(appAddress, stubAddress);

	const stubBin = join(runDir, 'stub');
	const build = spawnSync('go', ['build', '-o', stubBin, './testdata/stub'], {
		cwd: repoRoot,
		stdio: 'inherit'
	});
	if (build.status !== 0) throw new Error('the stub upstream did not build');

	let app: ChildProcess | null = null;
	let stub: ChildProcess | null = null;
	const heartbeat = setInterval(writeHostSnapshot, 30_000);
	heartbeat.unref();

	try {
		// The stub upstream and the stub executor are one process: the suite needs both, and two
		// binaries for a fixture would be two things to keep in step.
		stub = await start(stubBin, [`-addr=${stubAddress}`, `-control=${controlDir}`], stubLog);
		await waitFor(`${stubURL}/healthz`, 'the stub upstream', stub, stubLog);

		app = await start(binary, ['run', '--config', configPath], appLog);
		await waitFor(`${baseURL}/healthz`, 'meridian', app, appLog);
		await waitForFirstPoll(baseURL, app);

		const run: RunState = { baseURL, stubURL, configPath };
		writeFileSync(statePath, JSON.stringify(run, null, 2));
	} catch (error) {
		clearInterval(heartbeat);
		await stop(app);
		await stop(stub);
		throw error;
	}

	return async () => {
		clearInterval(heartbeat);
		await stop(app);
		await stop(stub);
	};
}
