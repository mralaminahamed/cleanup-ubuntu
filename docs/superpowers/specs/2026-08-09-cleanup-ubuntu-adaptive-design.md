# cleanup-ubuntu.sh — Adaptive Cleanup Design

**Date:** 2026-08-09
**Status:** Approved, ready for implementation planning
**Target file:** `/home/alamin/Projects/cleanup-ubuntu.sh` (single portable file, published as gist `b3ac5feada7e45ec834952162184bfec`)

---

## Problem

The script works but is not adaptive. Four concrete failures observed in a real session on 2026-08-09, where `/home` sat at 96% full and the Android emulator refused to start (`Not enough space to create userdata partition. Available: 5203.92 MB, need 12288.00 MB`):

1. **No notion of a goal.** The user needed 12 GB free. The script has no way to express that, so it runs every enabled step blindly whether or not the target was already met three steps ago.
2. **Big wins hide behind flags you must already know exist.** A bare run reported 1.1 GB reclaimable. The same machine actually had 4.6 GB behind `--claude-all --playwright` and another 5.1 GB behind `--jetbrains`. Discovering that required reading the source.
3. **No awareness of unknown junk.** `~/.config/Slack`, `~/.config/Postman`, `~/.local/share/zed`, `~/.config/goose`, and `~/.config/discord` held gigabytes in ordinary Electron cache subdirectories. The script has zero rules for any of them and said nothing.
4. **Filesystem-blind.** It measures only the filesystem holding `$HOME`. On this machine `/home` was 96% full while `/` sat at 67%. `--system` (apt cache, journal, snap revisions) frees space on `/` exclusively — running it would have reported success while freeing nothing on the partition that was actually full.

### Live safety bug

`--jetbrains` guards on the process regex `jetbrains|idea|pycharm|phpstorm|webstorm|goland|clion|rider|rubymine|datagrip`. Android Studio's process is `/usr/local/android-studio/bin/studio`, which matches none of them. The guard passes and the script deletes `~/.cache/JetBrains` — including the running Studio's indexes. Zed, VS Code, Cursor, Postman, Slack, and Discord are likewise unguarded.

---

## Design

### 1. Tier model

Every cleanup step becomes a **unit** with a declared tier and reversibility.

```
tier 0  free         pkg-mgr native cleans, /tmp stale, thumbnails, Trash
tier 1  regenerable  npm / uv / pip / go / composer / cargo caches,
                     node-gyp headers, phpactor index
tier 2  regenerable  generic app caches (pattern-matched, see §3),
                     playwright binaries, superseded claude plugin
                     versions, finished claude job dirs
tier 3  regenerable  IDE caches, browser HTTP caches
                     (cost: a reindex or a cold page load)
================== auto-escalation ceiling ==================
tier 4  LOSSY        claude session history, stale node_modules/vendor
tier 5  LOSSY        docker volumes, Claude Desktop VM bundles
```

**The ceiling is absolute for automation.** Pressure-driven and goal-driven escalation may select any unit in tiers 0–3. They may never select tier 4 or 5. Those are reported with their sizes and require an explicit `--allow-lossy`, which still prompts unless `--yes` is also given.

Rationale: everything at tier ≤ 3 costs time to rebuild. Everything at tier ≥ 4 costs information that cannot be rebuilt — `claude --resume` transcripts, database volumes, a `node_modules` tree for a project whose lockfile has since drifted.

### 2. Unit registry

Each unit declares:

| field | meaning |
|---|---|
| `id` | stable slug, e.g. `npm-cacache`, `jetbrains-cache` |
| `tier` | 0–5 per above |
| `reversible` | 1 for tiers 0–3, 0 for tiers 4–5 |
| `label` | human string for output |
| `mount` | filesystem the unit frees space on (resolved at probe time) |
| `probe` | function → bytes reclaimable, plus lock status. **Never deletes.** |
| `run` | function → performs the deletion |
| `flag` | the legacy opt-in flag that force-enables it, if any |

Splitting `probe` from `run` is the structural change that makes everything else possible. The current `reclaim()` fuses measurement and deletion, so the script cannot plan, cannot stop early, and cannot rank by size.

### 3. Planner control flow

```
1. survey    df every mounted filesystem; classify pressure per mount:
                <70%  relaxed
                70-85% normal
                85-95% high
                95%+  critical

2. resolve   target = --free N            (explicit, optionally :/mount)
                    | pressure-derived    (--auto: reach <85% on worst mount)
                    | none                (report only, delete nothing)

3. probe     every unit reports bytes + locked-by. No deletions occur.
                This is exactly what dry-run prints.

4. select    sort units by (tier asc, bytes desc)
                drop units whose mount is not the target mount
                drop units above the tier ceiling (unless --allow-lossy)
                drop locked units -> they move to the Locked section
                honour --tier N as a hard cap

5. execute   run selected units in order, re-check df after EACH unit
                stop the instant the target is met
                halt at the ceiling and report the shortfall

6. report    freed | still short by X | unlock potential | report-only list
```

### 4. Discovery

Two independent scans, both read-only during probe.

**`discover_app_caches()`** — depth-2 scan under `~/.config`, `~/.local/share`, `~/.cache`. A subdirectory is registered as a tier-2 unit when its basename matches a cache-semantic name:

```
Cache | Code Cache | GPUCache | ShaderCache | CachedData
| DawnCache | GrShaderCache | Crashpad | Cache_Data | logs
```

Names that are **never** matched, because they hold real state: `Local Storage`, `IndexedDB`, `Session Storage`, `databases`, `Service Worker`, `Local State`, `Preferences`.

If the owning application is running, the unit is not registered — it moves to the Locked section instead.

**`discover_heavyweights()`** — any directory over 200 MB not claimed by a registered unit is added to a **report-only** list, with a content heuristic classifying it `cache` / `data` / `mixed`. Nothing in this list is ever deleted. This is what surfaces `~/Downloads` at 5.5 GB — information the user needs, not a decision the script gets to make.

### 5. Lock detection

A table maps process regexes to the cache roots they own:

```
android-studio|studio\.sh|studio64   -> ~/.cache/JetBrains ~/.config/JetBrains
jetbrains|idea|pycharm|phpstorm|
  webstorm|goland|clion|rider|
  rubymine|datagrip                  -> ~/.cache/JetBrains ~/.config/JetBrains
zed                                  -> ~/.local/share/zed ~/.cache/zed
code|cursor                          -> ~/.config/Code ~/.config/Cursor
chrome                               -> ~/.cache/google-chrome ~/.config/google-chrome
brave                                -> ~/.cache/BraveSoftware ~/.config/BraveSoftware
firefox                              -> ~/.cache/mozilla
slack | discord | postman | goose | figma-linux -> respective config dirs
```

Adding `android-studio` here fixes the live bug. Any unit whose path falls under a locked root is skipped and reported:

```
== Locked by running apps ==
  5.1G  ~/.cache/JetBrains
        held by: android-studio (pid 324491)
        quit it, then re-run with: --jetbrains

  unlock 6.2G by closing 2 apps
```

### 6. CLI surface

Backward compatible. Every existing flag keeps its current meaning; passing a unit's flag force-includes that unit regardless of tier logic.

**New**

```
--free SIZE          target free space on the fs holding $HOME.
                     --free SIZE:/path targets another mount.
--auto               pressure-driven. Picks tiers itself; no flags needed.
--tier N             hard cap: never select above tier N.
--allow-lossy        permit tiers 4-5 (still prompts unless --yes).
--discover           run the discovery scans (implied by --auto).
--json               machine-readable probe output, for cron/monitoring.
```

**Changed**

```
--yes        now only skips prompts within already-allowed tiers.
             It is no longer a blanket "destroy anything" switch.
--system     skipped automatically when / is not the pressured mount.
```

**Unchanged:** `--apply --docker --docker-all --docker-volumes --jetbrains --browsers --playwright --deps --sites-idle --claude-vm --claude-jobs --claude-plugins --claude-history --claude-all --help`

Canonical new invocation: `cleanup-ubuntu.sh --apply --free 12G`

### 7. Structure

The script stays a **single file**. It is distributed as a gist and copy-pasted onto machines; splitting it into `lib/*.sh` would break that, which is the whole point of the artifact. The tier registry and planner live inside it as a data table plus a small engine, replacing the current straight-line section sequence.

Expected growth: roughly 573 → 850 lines.

---

## Testing

No test harness exists for this script and adding bats is out of scope. Instead, a `--self-test` mode builds a throwaway fixture tree under a temp root and asserts:

1. `probe` never deletes — fixture byte count is identical before and after a probe pass.
2. Goal-mode stops early — with a target satisfiable at tier 1, no tier-2 unit runs.
3. The ceiling holds — `--apply --yes --free <huge>` never selects a tier-4/5 unit.
4. `--allow-lossy --yes` does select them.
5. Locked units are skipped — with a fake running-process matcher, the owned unit is not run and appears in the Locked section.
6. Discovery never matches protected names — a fixture containing `Local Storage`, `IndexedDB`, and `Service Worker` yields zero units.
7. Mount filter works — a unit tagged to a non-target mount is not selected.
8. Legacy flags still force-include their unit even when tier logic would exclude it.

`--self-test` must run without touching the real `$HOME`, and must fail loudly with a non-zero exit if any assertion fails.

---

## Explicit non-goals

- No config file. Flags and auto-detection only.
- No scheduling, daemon, or systemd timer.
- No deletion of anything under `~/Downloads`, `~/Sites`, `~/Projects`, `~/MEGA`, or `~/.ollama` — these remain report-only or untouched, as today.
- No splitting into multiple files.
- No progress bars or TUI.

## Open risk

`discover_app_caches()` deletes paths chosen by pattern rather than by an explicit rule. The protected-name list in §4 is the only thing standing between it and real application state. That list must be treated as security-critical, and self-test assertion 6 exists specifically to guard it.
