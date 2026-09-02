# cleanup-ubuntu

Process-aware disk cleanup for Ubuntu / Debian. Safe by default: dry-run unless
told otherwise, reclaims regenerable caches only, and never touches source
trees, documents, LLM models, or databases.

Two implementations live here:

| | `cleanup-ubuntu.sh` | `ubclean` (Go) |
|---|---|---|
| Status | shipping, feature-complete | core complete, porting continues |
| Install | copy one file, run it | `go build ./cmd/ubclean` |
| Dry run of a full machine | ~3 min | ~2 s |
| Tests | 201 built-in assertions | `go test ./...` |

## The shell version

```bash
./cleanup-ubuntu.sh              # dry-run — show what would be freed
./cleanup-ubuntu.sh --apply      # actually clean
./cleanup-ubuntu.sh --help       # full flag reference
./cleanup-ubuntu.sh --self-test  # run the built-in suite
```

Published as gist [`b3ac5feada7e45ec834952162184bfec`](https://gist.github.com/b3ac5feada7e45ec834952162184bfec).
It is deliberately a single self-contained file so it can be copy-pasted onto a
machine without cloning anything, and read in full before being trusted with
`--apply`.

## The Go version

```bash
go build -o ubclean ./cmd/ubclean

./ubclean clean                       # dry-run report
./ubclean clean --apply               # clean, prompting first
./ubclean clean --free 12G            # clean until 12G is free, then stop
./ubclean clean --discover --gradle   # include unknown caches and Gradle
./ubclean status                      # filesystems and free space
./ubclean analyze --min 1G            # largest directories, deletes nothing
./ubclean history                     # what past runs actually deleted
```

Selection options the shell version does not have:

```bash
--only pip-cache --only 'xdg-*'   # restrict the run
--exclude 'npm-*'                 # drop units by id or glob
--workers 12                      # size the probe pool
```

Set `UBCLEAN_NO_OPLOG=1` to disable the operations log.

### Why a port

Probing is `du`-bound and every unit is independent, so it parallelises. That is
the whole speed difference above. The other win is structural: ten parallel
associative arrays keyed by unit id became one struct, so adding a field no
longer means editing the registrar, the reset helper and the JSON emitter in
lockstep.

There are no third-party dependencies. For a tool that deletes files as root,
an empty `go.mod` require block is a feature.

### Not yet ported

The Go version does **not** yet cover, and the shell version does:

- `--system`: apt cache, journal vacuum, old snap revisions
- `--deps` / `--sites-idle`: stale `node_modules` and `vendor` directories
- `--claude-jobs` / `--claude-plugins` / `--claude-history`
- per-mount pressure classification (`--auto` currently uses a crude target)

Use `cleanup-ubuntu.sh` for those until they land.

## Safety model

Both implementations share it:

- **Dry run by default.** Nothing is deleted without `--apply`.
- **Tiers.** Units are ordered from "costs nothing" to "may be irreplaceable".
  The planner walks tiers in order and will not cross a ceiling.
- **Reversibility is absolute.** An opt-in flag raises the tier ceiling but can
  never authorise a unit that destroys information; only `--allow-lossy` does.
- **Locks.** A cache belonging to a running application is skipped, and the
  report names the app and pid so you know what to quit.
- **Probe never deletes.** Everything reachable from the measuring phase is
  read-only, asserted by tests in both implementations.
- **Protected names.** `Local Storage`, `IndexedDB`, `Cookies`, `Login Data`
  and friends are never matched as caches, in any casing.

## Layout

```
cleanup-ubuntu.sh        the shell tool, one self-contained file
cmd/ubclean/             the Go CLI
internal/unit/           unit model and registry
internal/catalog/        the table of things worth reclaiming
internal/probe/          parallel measurement
internal/lock/           running-application detection
internal/plan/           tier ceiling and selection
internal/runner/         execution, with a protected-path backstop
internal/discover/       caches with no hardcoded rule
internal/report/         text and JSON output
internal/oplog/          append-only record of what was deleted
```

## License

MIT — see [LICENSE](LICENSE).
