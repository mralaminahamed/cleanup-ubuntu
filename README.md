# cleanup-ubuntu

Process-aware disk cleanup for Ubuntu / Debian. Safe by default: dry-run unless
told otherwise, reclaims regenerable caches only, and never touches source
trees, documents, LLM models, or databases.

```bash
./cleanup-ubuntu.sh              # dry-run — show what would be freed
./cleanup-ubuntu.sh --apply      # actually clean
./cleanup-ubuntu.sh --apply --free 12G   # clean until 12G is free, then stop
./cleanup-ubuntu.sh --help       # full flag reference
```

Published as gist [`b3ac5feada7e45ec834952162184bfec`](https://gist.github.com/b3ac5feada7e45ec834952162184bfec).
The script is deliberately a single self-contained file so it can be copy-pasted
onto a machine without cloning anything.

## Adaptive cleanup

Every step is a *unit* carrying a tier, a reversibility flag and the filesystem
it frees space on. A planner probes them all without deleting, then picks what
to run:

- `--free 12G` cleans until the target is met and stops. `--free 12G:/mnt/data`
  targets another mount.
- `--auto` reads disk pressure and picks tiers itself.
- `--discover` finds caches belonging to apps the script has no rule for, and
  reports large directories nothing covers — without touching them.
- `--tier N` caps how far it will go; `--json` emits machine-readable output.

Tiers 0–3 are regenerable — worst case you wait for a rebuild, so automation may
run them. Tiers 4–5 lose information (`claude --resume` history, dependency
trees, docker volumes) and are never selected automatically: they need
`--allow-lossy`, and each one is still confirmed individually unless `--yes`.

Caches owned by a running app are never deleted. The report names the process
holding them and how much closing it would unlock.

## Tests

```bash
./cleanup-ubuntu.sh --self-test
```

Runs against throwaway fixtures, never the real `$HOME`. Exits non-zero on any
failure.

## License

MIT — see [LICENSE](LICENSE).
