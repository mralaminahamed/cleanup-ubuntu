# cleanup-ubuntu

Process-aware disk cleanup for Ubuntu / Debian. Safe by default: dry-run unless
told otherwise, reclaims regenerable caches only, and never touches source
trees, documents, LLM models, or databases.

```bash
./cleanup-ubuntu.sh              # dry-run — show what would be freed
./cleanup-ubuntu.sh --apply      # actually clean
./cleanup-ubuntu.sh --help       # full flag reference
```

Published as gist [`b3ac5feada7e45ec834952162184bfec`](https://gist.github.com/b3ac5feada7e45ec834952162184bfec).
The script is deliberately a single self-contained file so it can be copy-pasted
onto a machine without cloning anything.

## Layout

```
cleanup-ubuntu.sh                    the whole tool
docs/superpowers/specs/              design specs
docs/superpowers/plans/              implementation plans
```

## In flight

Adaptive cleanup — goal-driven (`--free 12G`), pressure-driven (`--auto`),
discovery of unknown app caches, and per-filesystem targeting.
See `docs/superpowers/specs/2026-08-09-cleanup-ubuntu-adaptive-design.md`.
