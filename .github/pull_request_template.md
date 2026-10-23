## Summary

<!-- What changes and why, in a few sentences. Link the decision or issue if there is one. -->

## Changes

<!-- The notable pieces, one bullet each. Skip anything the diff already makes obvious. -->

-

## Standards checklist

<!-- Delete lines that do not apply; leave a note if you skip one that does. -->

- [ ] Tests were written first and seen failing; bug fixes include a test that reproduces the bug
- [ ] New or changed parsers and security-boundary validators have a fuzz target, and any committed crasher lives under `testdata/fuzz`
- [ ] `scripts/check` is green locally and each commit passes `scripts/check --fast`
- [ ] Commits are small, one logical change each, in Conventional Commits form
- [ ] Docs that this change made false are fixed in the same PR; new design choices are appended to `docs/DECISIONS.md`

## Hard constraints

<!-- See CLAUDE.md. Tick to confirm the change does not touch these, or explain below. -->

- [ ] No service name, host, port, icon or quirk added to Go or Svelte (config only)
- [ ] The container still gets no Docker socket, root, shell or arbitrary command execution
- [ ] No authentication, token or session added; no state-changing button on the launcher
- [ ] No new dependency outside the allowlist in `docs/STANDARDS.md` section 9 (or a new decision covers it)
- [ ] No secret, real `config.yaml` or generated output committed

## Security impact

<!-- Delete this section if the change touches no trust boundary. Otherwise: what input is now
     accepted, where it is validated, and which fuzz target covers it. For anything under
     internal/hostexec, internal/hostprobe, deploy/ or /etc/meridian, say what a hostile container
     could now do that it could not before (ideally: nothing). -->

## How it was verified

<!-- Commands run, what you saw, anything that needs the real box and was not exercised. -->
