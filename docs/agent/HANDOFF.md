# Handoff

Read this first in every session. It is **rewritten, not appended**, at the end of each session; the
history is in git. Keep it under about 40 lines: it is for a cold start with an empty context.

## Template

```
## Last session: S<n> (<date>)
Finished: T## ... (ticked in TASKS.md)
Partial: none | T## and a checklist of remaining behaviours, with the files holding the finished part

## Next
Session S<n+1>, first task T##. One line on what it needs first.

## State of the tree
Clean and green under `scripts/check` | what is red and why

## Gotchas
Things the next session would otherwise rediscover: a surprising behaviour, a tool quirk, an
assumption a test depends on, a decision made in the middle of a task.

## For the owner
Anything that needs your hands (a secret, a GitHub setting, a [human] gate) or your call
(a pending item in OPEN-QUESTIONS.md).
```

---

## Last session: S3 (2026-10-24)

Finished: T09, T10, T11 (ticked). `internal/probe` now has the fetch client (`fetch.go`), the reason
code table (`reasons.go`), all six health kinds (`health.go`), the UDP DNS resolver (`dns.go`) and all
five vital kinds (`vital.go`), plus `registry_test.go` and the fuzz targets `FuzzBuildRequest`,
`FuzzHTTPJSON`, `FuzzDNSResponse`, `FuzzVitals` with committed crashers.

Partial: none.

## Next

Session **S4**, first task **T12** (result model, merge rules, store), then T13. Read
`docs/agent/03-probe.md` from T12 and decisions #35 and #40 first.

## State of the tree

Clean and green under the full `scripts/check` (go, security; web and e2e still skip). 42 commits
unpushed.

## Gotchas

- `Env` carries `HostMaxAge` and `Now` as well as the three seams. A host probe with either missing
  reports `unknown` / `host_unavailable`: deliberately fail-safe, so T13 must set both from
  `HostStatus.MaxAge` or every unit and tailnet tile reads unknown.
- Vitals signal reasons with sentinels (`ErrMissingField`, `ErrUnexpectedType`, `ErrUnexpectedValue`,
  `ErrBadValue`, plus `ErrHostStale` / `ErrHostUnavailable`). `probe.CodeOf(err)` turns any of them,
  and the fetch errors, into a reason code. Merge should use it, never match on message text.
- `Probe.Has(key)` reads the `Set` map filled by the YAML unmarshaller, so a probe built in code must
  fill `Set` (`keys(...)` helper in the tests) or `equals`/`field` are silently treated as absent.
- The fetch client follows no redirects, caps bodies at `probe.MaxBodySize`, and reads credentials
  from the getenv func at request time. Wrap it in `probe.Memo` once per refresh cycle, never across.
- The stage doc's code list gained two codes: `unreachable` (a host that will not resolve) and `off`
  (a reported boolean that is false). `off` is what a false `host_field` health returns.
- `go.mod` now says `go 1.26.10` because govulncheck flagged 17 stdlib advisories in 1.26.1. CI
  installs the toolchain from go.mod, so the directive is the single pin. Local `go` auto-downloads it.
- `gocritic`'s `hugeParam` is disabled repo-wide: `config.Probe` travels by value through the runner
  seam so a runner cannot mutate config.

## For the owner

- Nothing is pushed. The first push to `main` is CI's first real run.
- Cosmetic: `suffix: "% blocked"` in the worked example renders as `30 % blocked`, because
  `format.Compose` always joins with a space. Change the config or the helper if that reads badly.
