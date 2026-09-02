# reclaim

Process-aware storage reclamation for Ubuntu and Debian. Safe by default: it
measures and reports unless told otherwise, reclaims regenerable caches only,
and never touches source trees, documents, LLM models, or databases.

```bash
go build -o reclaim ./cmd/reclaim

reclaim clean                     # dry-run report, deletes nothing
reclaim clean --apply             # reclaim, prompting first
reclaim status                    # filesystems and disk pressure
reclaim analyze --min 1G          # largest directories, advisory only
reclaim history                   # what past runs actually deleted
```

## Usage

### Targets

```bash
reclaim clean --free 12G          # clean until 12G is free, then stop
reclaim clean --auto              # read disk pressure and pick a target
reclaim clean --tier 2            # never escalate past tier 2
```

`--auto` reads the worst filesystem and lets how full it is decide how hard to
try: a comfortable disk gets only the free tiers, a critical one earns a cold
reload.

### Scope

```bash
reclaim clean --discover          # also claim caches with no hardcoded rule
reclaim clean --only pip-cache --only 'xdg-*'
reclaim clean --exclude 'npm-*'
reclaim clean --workers 12        # size the probe pool
```

### Opt-in groups

Each of these raises the tier ceiling for its own units only:

| Flag | Reclaims |
|---|---|
| `--gradle` | `~/.gradle/caches`, `~/.gradle/wrapper` |
| `--maven` | `~/.m2/repository` |
| `--jetbrains` | JetBrains IDE caches |
| `--browsers` | Chrome, Brave and Firefox HTTP caches |
| `--playwright` | Playwright browser binaries |
| `--system` | apt cache, journal vacuum, superseded snap revisions |
| `--claude-jobs`, `--claude-plugins` | Claude Code scratch and plugin cache |
| `--docker`, `--docker-volumes` | Docker prune (volumes may hold databases) |
| `--sites-idle N` | dependency trees of projects idle N days |

Irreversible units additionally require `--allow-lossy`.

Set `RECLAIM_NO_OPLOG=1` to disable the operations log.

## Safety model

- **Dry run by default.** Nothing is deleted, and no command runs, without
  `--apply`.
- **Tiers.** Units run from "costs nothing" to "may be irreplaceable", in that
  order, and the planner will not cross the ceiling it was given.
- **Reversibility is absolute.** An opt-in flag raises the tier ceiling but can
  never authorise a unit that destroys information. Only `--allow-lossy` does,
  and a test pins that distinction.
- **Locks.** A cache belonging to a running application is skipped, and the
  report names the app and its pid so you know what to quit. The tool excludes
  its own process, so its command line cannot lock it out of its own work.
- **Probing never deletes.** Everything reachable from the measuring phase is
  read-only, asserted by a test that walks the tree before and after.
- **Protected names.** `Local Storage`, `IndexedDB`, `Cookies`, `Login Data`
  and friends are never treated as caches, in any casing.
- **Protected paths.** A backstop refuses `/`, top-level system directories and
  `$HOME` however a unit is defined, so a malformed unit cannot aim the deleter
  at the wrong tree.

## Design

Probing is `du`-bound and every unit is independent, so it runs on a worker
pool. A full discovery run over this machine takes about 1.7 seconds.

There are no third-party dependencies. For a tool that deletes files as root,
an empty `require` block is a feature.

```
cmd/reclaim/         the CLI
internal/unit/       unit model and registry
internal/catalog/    the table of things worth reclaiming
internal/system/     apt, journal and snap units
internal/scan/       dependency trees of idle projects
internal/probe/      parallel measurement
internal/lock/       running-application detection
internal/plan/       tier ceiling and selection
internal/runner/     execution, with a protected-path backstop
internal/discover/   caches with no hardcoded rule
internal/report/     text and JSON output
internal/oplog/      append-only record of what was deleted
internal/fsutil/     sizes, mounts and pressure
```

## Development

```bash
go test ./...          # unit and end-to-end tests
go test ./... -race
go vet ./...
```

The CLI tests build the real binary and run it against a fixture `HOME`, so flag
parsing, exit codes and JSON validity are covered end to end.

### History

This began as a single self-contained bash script, which reached 1615 lines and
201 built-in assertions before being ported. The shell version is preserved in
git history if you need it:

```bash
git show 67fd60c:reclaim.sh > reclaim.sh
```

## License

MIT — see [LICENSE](LICENSE).
