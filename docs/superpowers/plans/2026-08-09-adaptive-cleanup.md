# Adaptive Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `cleanup-ubuntu.sh` adaptive — able to clean toward a free-space target, escalate on its own under disk pressure, discover caches it has no hardcoded rule for, and target the specific filesystem that is actually full.

**Architecture:** Replace the current straight-line sequence of cleanup sections with a **unit registry** plus a **planner engine**. Each cleanup step registers itself as a unit carrying a tier, a reversibility flag, a mount, a probe function, and a run function. The planner surveys mounts, resolves a target, probes every unit without deleting, selects units by tier and mount, then executes them one at a time re-checking `df` after each — stopping the moment the target is met and refusing to cross the reversible/lossy tier ceiling.

**Tech Stack:** Bash 4+ (associative arrays required), coreutils (`df`, `du`, `find`, `stat`, `numfmt`, `truncate`, `mktemp`), `pgrep`. No external dependencies, no test framework — testing is a built-in `--self-test` mode.

## Global Constraints

- **The script stays one self-contained file.** It is distributed as gist `b3ac5feada7e45ec834952162184bfec` and copy-pasted onto machines. Never split into `lib/*.sh`.
- **Bash 4+ only.** Associative arrays (`declare -A`) are required. Keep the `#!/usr/bin/env bash` shebang; do not target POSIX sh.
- **`set -uo pipefail` stays as-is.** Do not add `-e` — the script deliberately continues past individual failures.
- **Probe must never delete.** Any function reachable from the probe phase is read-only. This is asserted by self-test 1.
- **The tier ceiling is absolute for automation.** Pressure-driven and goal-driven selection may only pick units with `reversible=1` (tiers 0–3). Tiers 4–5 require the explicit `--allow-lossy` flag.
- **Backward compatibility is required.** Every existing flag keeps working and force-includes its unit regardless of tier logic: `--apply --docker --docker-all --docker-volumes --jetbrains --browsers --playwright --deps --sites-idle --claude-vm --claude-jobs --claude-plugins --claude-history --claude-all --help`.
- **Never run as root.** The existing `[[ $EUID -eq 0 ]]` refusal stays.
- **Protected cache-adjacent names** (never matched by discovery): `Local Storage`, `IndexedDB`, `Session Storage`, `databases`, `Service Worker`, `Local State`, `Preferences`. This list is security-critical.
- **Paths may contain spaces.** `Code Cache` and `Application Support` are real. Store path lists newline-delimited and read them with `readarray`; never rely on word-splitting.
- Commit with the repo-local git identity already configured (`Al Amin Ahamed <alamin.ahamed.dev@gmail.com>`). Do not pass `-c user.email` overrides.

---

## File Structure

One file: `cleanup-ubuntu.sh`. The plan reorganises it into these sections, in this order. Line ranges refer to the current 573-line version.

| Section | Current | After |
|---|---|---|
| Header comment / usage | 1–59 | rewritten in Task 11 |
| Flag parsing | 60–104 | extended in Task 11 |
| UI helpers (`section`, `info`, `human`, …) | 106–127 | unchanged |
| **Self-test harness** | — | new, Task 1 |
| **Mount survey + pressure** | — | new, Task 2 |
| **Unit registry + probe/run** | replaces `reclaim()` 133–148 | new, Task 3 |
| **Lock table + detection** | replaces `is_running()` 127 | new, Task 4 |
| **Unit definitions** | replaces sections 1–11, lines 194–557 | new, Task 5 |
| **Discovery** | — | new, Tasks 6–7 |
| **Planner** | — | new, Task 8 |
| **Executor** | — | new, Task 9 |
| **Reporting** | replaces summary 559–573 | new, Task 10 |

Order matters: helpers before registry, registry before unit definitions, definitions before planner. The self-test harness comes first so every subsequent task has a working test cycle.

---

### Task 1: Self-test harness

Every later task needs somewhere to put its assertions. This task builds that, and proves it works by asserting on the existing `human()` helper.

**Files:**
- Modify: `cleanup-ubuntu.sh` — insert after the UI helpers block (currently ends line 127)

**Interfaces:**
- Consumes: nothing
- Produces: `st_assert <desc> <actual> <expected>`, `st_assert_ne <desc> <actual> <unexpected>`, `st_fixture` (creates `$ST_ROOT` and echoes it), `st_cleanup`, `st_run` (the `--self-test` entry point), counters `ST_PASS` / `ST_FAIL`

- [ ] **Step 1: Write the failing test**

Add to `cleanup-ubuntu.sh` immediately after the `is_running()` definition:

```bash
# ---------------------------------------------------------------------------------
# self-test harness (only active under --self-test)
# ---------------------------------------------------------------------------------
ST_PASS=0 ST_FAIL=0 ST_ROOT=""

st_assert() { # st_assert <desc> <actual> <expected>
  if [[ "$2" == "$3" ]]; then
    ST_PASS=$((ST_PASS + 1)); printf '  %sok%s   %s\n' "$C_GRN" "$C_RESET" "$1"
  else
    ST_FAIL=$((ST_FAIL + 1))
    printf '  %sFAIL%s %s\n       got:      %s\n       expected: %s\n' \
      "$C_RED" "$C_RESET" "$1" "$2" "$3"
  fi
}

st_assert_ne() { # st_assert_ne <desc> <actual> <unexpected>
  if [[ "$2" != "$3" ]]; then
    ST_PASS=$((ST_PASS + 1)); printf '  %sok%s   %s\n' "$C_GRN" "$C_RESET" "$1"
  else
    ST_FAIL=$((ST_FAIL + 1))
    printf '  %sFAIL%s %s\n       got %s, which should have differed\n' \
      "$C_RED" "$C_RESET" "$1" "$2"
  fi
}

# Build a deterministic fixture tree. Sparse files via truncate, so sizes are
# exact and creation is instant. Echoes the root.
st_fixture() {
  ST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/cleanup-selftest.XXXXXX") || return 1
  mkdir -p "$ST_ROOT/.cache/npm" "$ST_ROOT/.config/FakeApp/Cache" \
           "$ST_ROOT/.config/FakeApp/Local Storage" \
           "$ST_ROOT/.config/FakeApp/IndexedDB"
  truncate -s 1M  "$ST_ROOT/.cache/npm/blob"
  truncate -s 2M  "$ST_ROOT/.config/FakeApp/Cache/entry"
  truncate -s 4M  "$ST_ROOT/.config/FakeApp/Local Storage/leveldb.ldb"
  truncate -s 8M  "$ST_ROOT/.config/FakeApp/IndexedDB/store.db"
  printf '%s' "$ST_ROOT"
}

st_cleanup() { [[ -n "$ST_ROOT" && -d "$ST_ROOT" ]] && rm -rf -- "$ST_ROOT"; ST_ROOT=""; }

st_run() {
  printf '%s%s self-test %s\n' "$C_B" "$C_CYN" "$C_RESET"
  local t
  for t in $(declare -F | awk '{print $3}' | grep '^sttest_' | sort); do
    printf '\n%s-- %s --%s\n' "$C_DIM" "${t#sttest_}" "$C_RESET"
    "$t"
    st_cleanup
  done
  printf '\n  %spassed %d%s  %sfailed %d%s\n' \
    "$C_GRN" "$ST_PASS" "$C_RESET" "$C_RED" "$ST_FAIL" "$C_RESET"
  [[ $ST_FAIL -eq 0 ]]
}

sttest_harness() {
  st_assert "human() formats bytes"     "$(human 1048576)" "1.0MiB"
  st_assert "human() handles zero"      "$(human 0)"       "0B"
  local root; root=$(st_fixture)
  st_assert "fixture root exists"       "$([[ -d "$root" ]] && echo yes)" "yes"
  st_assert "fixture npm blob is 1M"    "$(path_bytes "$root/.cache/npm/blob")" "1048576"
  st_assert "fixture protected dir made" \
    "$([[ -d "$root/.config/FakeApp/Local Storage" ]] && echo yes)" "yes"
}
```

Add the flag. In the `while [[ $# -gt 0 ]]` case block, before the `-h|--help` line:

```bash
    --self-test)      SELF_TEST=1 ;;
```

And declare it with the other flag defaults (near line 65):

```bash
SELF_TEST=0
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test`

Expected: FAIL — nothing invokes `st_run` yet, so the script runs a normal dry-run cleanup instead of the test suite. You will see the usual `== JS / Node package caches ==` output, not `self-test`.

- [ ] **Step 3: Write minimal implementation**

Wire the entry point. Insert immediately after the preflight `[[ $EUID -eq 0 ]]` check (currently line 182), before `HOME_FS=$(...)`:

```bash
if [[ $SELF_TEST -eq 1 ]]; then
  st_run; exit $?
fi
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test`

Expected: PASS. Output shows `-- harness --` with 5 `ok` lines, then `passed 5  failed 0`. Exit status 0 — verify with `echo $?`.

Then confirm it did not touch the real home: `bash cleanup-ubuntu.sh --self-test && ls ~/.npm/_cacache >/dev/null && echo "real home intact"`

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "test: add --self-test harness with fixture builder"
```

---

### Task 2: Mount survey and pressure classification

**Files:**
- Modify: `cleanup-ubuntu.sh` — insert after the self-test harness

**Interfaces:**
- Consumes: nothing
- Produces: `mount_of <path>` → mountpoint string; `avail_bytes <path>` → integer; `usepct <path>` → integer 0–100; `pressure_of <path>` → one of `relaxed|normal|high|critical`; `parse_size <str>` → integer bytes, returns 1 on bad input; `survey_mounts` → prints the per-mount table

- [ ] **Step 1: Write the failing test**

Append this test function to the self-test section:

```bash
sttest_mounts() {
  st_assert "parse_size plain bytes"  "$(parse_size 1024)"  "1024"
  st_assert "parse_size K"            "$(parse_size 4K)"    "4096"
  st_assert "parse_size M"            "$(parse_size 2M)"    "2097152"
  st_assert "parse_size G"            "$(parse_size 12G)"   "12884901888"
  st_assert "parse_size lowercase g"  "$(parse_size 12g)"   "12884901888"
  parse_size "12.5G" >/dev/null 2>&1
  st_assert "parse_size rejects decimal" "$?" "1"
  parse_size "banana" >/dev/null 2>&1
  st_assert "parse_size rejects garbage" "$?" "1"

  st_assert "mount_of / is /"        "$(mount_of /)" "/"
  st_assert_ne "avail_bytes / nonzero" "$(avail_bytes /)" "0"

  local p; p=$(usepct /)
  st_assert "usepct in range" "$([[ "$p" -ge 0 && "$p" -le 100 ]] && echo yes)" "yes"

  # pressure thresholds are pure arithmetic — test the classifier directly
  st_assert "pressure 50 relaxed"  "$(pressure_from_pct 50)"  "relaxed"
  st_assert "pressure 69 relaxed"  "$(pressure_from_pct 69)"  "relaxed"
  st_assert "pressure 70 normal"   "$(pressure_from_pct 70)"  "normal"
  st_assert "pressure 84 normal"   "$(pressure_from_pct 84)"  "normal"
  st_assert "pressure 85 high"     "$(pressure_from_pct 85)"  "high"
  st_assert "pressure 94 high"     "$(pressure_from_pct 94)"  "high"
  st_assert "pressure 95 critical" "$(pressure_from_pct 95)"  "critical"
  st_assert "pressure 100 critical" "$(pressure_from_pct 100)" "critical"
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | grep -c FAIL`

Expected: non-zero count, with errors like `parse_size: command not found`.

- [ ] **Step 3: Write minimal implementation**

Insert after the self-test harness block:

```bash
# ---------------------------------------------------------------------------------
# filesystem survey
# ---------------------------------------------------------------------------------
mount_of()    { df -P "$1" 2>/dev/null | awk 'NR==2{print $6}'; }
fs_of()       { df -P "$1" 2>/dev/null | awk 'NR==2{print $1}'; }
avail_bytes() { local k; k=$(df -P "$1" 2>/dev/null | awk 'NR==2{print $4}'); echo $(( ${k:-0} * 1024 )); }
usepct()      { df -P "$1" 2>/dev/null | awk 'NR==2{gsub(/%/,"",$5); print $5+0}'; }

pressure_from_pct() {
  local p=${1:-0}
  if   (( p >= 95 )); then echo critical
  elif (( p >= 85 )); then echo high
  elif (( p >= 70 )); then echo normal
  else                     echo relaxed
  fi
}
pressure_of() { pressure_from_pct "$(usepct "$1")"; }

# "12G" -> 12884901888. Integer + optional K/M/G/T only; decimals are rejected
# because bash has no float arithmetic and a silently truncated target is worse
# than an error.
parse_size() {
  local s="${1:-}" n unit
  s=$(printf '%s' "$s" | tr '[:lower:]' '[:upper:]')
  [[ "$s" =~ ^([0-9]+)([KMGT]?)B?$ ]] || return 1
  n="${BASH_REMATCH[1]}"; unit="${BASH_REMATCH[2]}"
  case "$unit" in
    '') echo "$n" ;;
    K)  echo $(( n * 1024 )) ;;
    M)  echo $(( n * 1024 * 1024 )) ;;
    G)  echo $(( n * 1024 * 1024 * 1024 )) ;;
    T)  echo $(( n * 1024 * 1024 * 1024 * 1024 )) ;;
  esac
}

# Print every real filesystem with its pressure. Skips pseudo/loop mounts so the
# table shows only things a human could actually free space on.
survey_mounts() {
  section "Filesystems"
  local src size used avail pct mp pr
  while read -r src size used avail pct mp; do
    [[ "$src" == Filesystem ]] && continue
    case "$src" in /dev/loop*|tmpfs|devtmpfs|efivarfs|none) continue ;; esac
    pct=${pct%\%}
    pr=$(pressure_from_pct "$pct")
    case "$pr" in
      critical) printf '  %s%-28s %5s used %5s free  %3s%%  %s%s\n' "$C_RED" "$mp" "$used" "$avail" "$pct" "$pr" "$C_RESET" ;;
      high)     printf '  %s%-28s %5s used %5s free  %3s%%  %s%s\n' "$C_YEL" "$mp" "$used" "$avail" "$pct" "$pr" "$C_RESET" ;;
      *)        printf '  %-28s %5s used %5s free  %3s%%  %s\n' "$mp" "$used" "$avail" "$pct" "$pr" ;;
    esac
  done < <(df -PH 2>/dev/null)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | tail -3`

Expected: `failed 0`.

Eyeball the survey too — temporarily call `survey_mounts` from the top of `st_run`, confirm your `/home` and `/` both appear with sane percentages, then remove the temporary call.

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "feat: add per-mount survey and pressure classification"
```

---

### Task 3: Unit registry with probe/run split

This is the structural heart of the change. `reclaim()` currently measures and deletes in one pass, which is why the script cannot plan. Split it.

**Files:**
- Modify: `cleanup-ubuntu.sh` — insert after the mount survey; leave the old `reclaim()` in place for now (Task 5 removes it)

**Interfaces:**
- Consumes: `mount_of`, `path_bytes`, `human`
- Produces:
  - `register_unit <id> <tier> <reversible> <label> <kind> <payload> [flag]` — `kind` is `paths` or `cmd`; for `paths` the payload is a newline-delimited path list, for `cmd` it is a shell command string
  - `U_IDS` array, and associative arrays `U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED`
  - `unit_paths <id>` — echoes the payload, one path per line
  - `probe_unit <id>` — sets `U_BYTES[$id]` and `U_MOUNT[$id]`, deletes nothing
  - `run_unit <id>` — performs the deletion, echoes bytes freed

- [ ] **Step 1: Write the failing test**

```bash
sttest_registry() {
  local root; root=$(st_fixture)

  U_IDS=(); unset U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED
  declare -gA U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED

  register_unit "fake-cache" 2 1 "fake app cache" paths \
    "$root/.config/FakeApp/Cache" "--fake"

  st_assert "unit registered"     "${U_IDS[0]}"          "fake-cache"
  st_assert "tier stored"         "${U_TIER[fake-cache]}" "2"
  st_assert "reversible stored"   "${U_REV[fake-cache]}"  "1"
  st_assert "flag stored"         "${U_FLAG[fake-cache]}" "--fake"

  # probe must measure without deleting
  local before after
  before=$(path_bytes "$root/.config/FakeApp/Cache")
  probe_unit "fake-cache"
  after=$(path_bytes "$root/.config/FakeApp/Cache")
  st_assert "probe reports size"     "${U_BYTES[fake-cache]}" "2097152"
  st_assert "PROBE DELETES NOTHING"  "$after"                 "$before"
  st_assert "probe resolved mount"   "$([[ -n "${U_MOUNT[fake-cache]}" ]] && echo yes)" "yes"

  # a path with a space must survive round-tripping
  register_unit "spacey" 2 1 "spacey" paths "$root/.config/FakeApp/Local Storage"
  probe_unit "spacey"
  st_assert "space-in-path measured" "${U_BYTES[spacey]}" "4194304"

  # run actually deletes and reports
  local freed; freed=$(run_unit "fake-cache")
  st_assert "run reports freed"   "$freed" "2097152"
  st_assert "run deleted the dir" "$([[ -e "$root/.config/FakeApp/Cache" ]] && echo yes || echo no)" "no"
  st_assert "run left siblings"   "$([[ -e "$root/.config/FakeApp/Local Storage" ]] && echo yes)" "yes"

  # multi-path unit sums correctly
  register_unit "multi" 1 1 "multi" paths \
    "$root/.cache/npm/blob
$root/.config/FakeApp/IndexedDB"
  probe_unit "multi"
  st_assert "multi-path sums" "${U_BYTES[multi]}" "9437184"
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | grep -A2 registry | head -20`

Expected: FAIL with `register_unit: command not found`.

- [ ] **Step 3: Write minimal implementation**

```bash
# ---------------------------------------------------------------------------------
# unit registry
#
# A "unit" is one cleanup step. Splitting probe (measure) from run (delete) is
# what lets the planner rank by size, stop early once a target is met, and print
# an honest dry-run.
# ---------------------------------------------------------------------------------
U_IDS=()
declare -A U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED

# register_unit <id> <tier> <reversible> <label> <kind> <payload> [flag]
#   kind=paths  payload = newline-delimited paths to delete
#   kind=cmd    payload = shell command run verbatim (native cache cleaners)
register_unit() {
  local id=$1
  U_IDS+=("$id")
  U_TIER[$id]=$2
  U_REV[$id]=$3
  U_LABEL[$id]=$4
  U_KIND[$id]=$5
  U_PAYLOAD[$id]=$6
  U_FLAG[$id]=${7:-}
  U_BYTES[$id]=0
  U_LOCKED[$id]=""
  U_MOUNT[$id]=""
}

unit_paths() { printf '%s\n' "${U_PAYLOAD[$1]}"; }

# Measure only. Nothing here may mutate the filesystem.
probe_unit() {
  local id=$1 total=0 b p
  local -a plist=()
  if [[ "${U_KIND[$id]}" == cmd ]]; then
    U_BYTES[$id]=0
    U_MOUNT[$id]=$(mount_of "$HOME")
    return 0
  fi
  readarray -t plist < <(unit_paths "$id")
  for p in "${plist[@]}"; do
    [[ -z "$p" || ! -e "$p" ]] && continue
    [[ -z "${U_MOUNT[$id]}" ]] && U_MOUNT[$id]=$(mount_of "$p")
    b=$(path_bytes "$p")
    total=$(( total + b ))
  done
  [[ -z "${U_MOUNT[$id]}" ]] && U_MOUNT[$id]=$(mount_of "$HOME")
  U_BYTES[$id]=$total
}

# Delete. Echoes bytes freed. Assumes probe_unit already ran.
run_unit() {
  local id=$1 p
  local -a plist=()
  if [[ "${U_KIND[$id]}" == cmd ]]; then
    eval "${U_PAYLOAD[$id]}" >/dev/null 2>&1
    echo 0; return 0
  fi
  readarray -t plist < <(unit_paths "$id")
  for p in "${plist[@]}"; do
    [[ -z "$p" || ! -e "$p" ]] && continue
    chmod -R u+w "$p" 2>/dev/null
    rm -rf -- "$p" 2>/dev/null
  done
  echo "${U_BYTES[$id]}"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | tail -3`

Expected: `failed 0`. The `PROBE DELETES NOTHING` assertion is the one that matters most — if it ever fails, stop and fix before continuing.

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "feat: add unit registry with probe/run separation"
```

---

### Task 4: Lock table and running-app detection

Fixes the live bug: `--jetbrains` currently deletes a running Android Studio's indexes because `/usr/local/android-studio/bin/studio` matches none of the guarded process names.

**Files:**
- Modify: `cleanup-ubuntu.sh` — insert after the unit registry; keep the old `is_running()` (Task 5 removes its last callers)

**Interfaces:**
- Consumes: `register_unit` internals (`U_LOCKED`), `probe_unit`
- Produces: `LOCK_RX` / `LOCK_ROOTS` parallel arrays; `running_procs` → newline-delimited `pid<TAB>cmdline`; `locked_by <path>` → `appname:pid` or empty; `apply_locks` → fills `U_LOCKED` for every registered unit

- [ ] **Step 1: Write the failing test**

```bash
sttest_locks() {
  # the regex table must recognise every launcher form Android Studio uses,
  # which is exactly the bug this fixes
  st_assert "android-studio bin path matches" \
    "$(lock_rx_matches '/usr/local/android-studio/bin/studio' && echo yes || echo no)" "yes"
  st_assert "studio.sh matches" \
    "$(lock_rx_matches '/opt/android-studio/bin/studio.sh' && echo yes || echo no)" "yes"
  st_assert "jetbrains idea still matches" \
    "$(lock_rx_matches '/opt/idea/bin/idea.sh' && echo yes || echo no)" "yes"
  st_assert "zed matches" \
    "$(lock_rx_matches '/usr/bin/zed' && echo yes || echo no)" "yes"
  st_assert "unrelated proc does not match" \
    "$(lock_rx_matches '/usr/bin/htop' && echo yes || echo no)" "no"
  st_assert "the cleanup script itself never matches" \
    "$(lock_rx_matches 'bash cleanup-ubuntu.sh --apply' && echo yes || echo no)" "no"

  # a unit under a locked root must be marked, and one outside must not
  local root; root=$(st_fixture)
  U_IDS=(); unset U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED
  declare -gA U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED

  register_unit "locked-one"   3 1 "locked"   paths "$root/.config/FakeApp/Cache"
  register_unit "unlocked-one" 1 1 "unlocked" paths "$root/.cache/npm"

  # inject a fake running app owning the FakeApp root
  LOCK_RX=("fakeapp") ; LOCK_ROOTS=("$root/.config/FakeApp") ; LOCK_NAME=("FakeApp")
  ST_FAKE_PROCS=$'4242\t/usr/bin/fakeapp --no-sandbox'
  apply_locks

  st_assert "unit under locked root is marked" "${U_LOCKED[locked-one]}"   "FakeApp:4242"
  st_assert "unit outside locked root is free" "${U_LOCKED[unlocked-one]}" ""
  ST_FAKE_PROCS=""
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | grep -A2 locks | head -20`

Expected: FAIL with `lock_rx_matches: command not found`.

- [ ] **Step 3: Write minimal implementation**

```bash
# ---------------------------------------------------------------------------------
# lock detection — which running app owns which cache root
#
# Three parallel arrays, index-aligned. The regex is matched against the full
# process cmdline. android-studio is listed separately from the jetbrains regex
# because its launcher path contains none of the JetBrains product names, which
# previously let the script delete a live Studio's indexes.
# ---------------------------------------------------------------------------------
LOCK_RX=(
  'android-studio|/studio\.sh|/studio64|bin/studio( |$)'
  'jetbrains|/idea|pycharm|phpstorm|webstorm|goland|clion|rider|rubymine|datagrip'
  '/zed( |$)|/zed-editor'
  '/code( |$)|/code-insiders|/cursor( |$)'
  '/chrome( |$)|/chromium( |$)'
  '/brave( |$)|brave-browser'
  '/firefox( |$)'
  '/slack( |$)'
  '/discord( |$)|/Discord( |$)'
  '/postman( |$)|/Postman( |$)'
  '/goose( |$)'
  'figma-linux|/figma( |$)'
)
LOCK_NAME=(
  "android-studio" "jetbrains-ide" "zed" "vscode" "chrome" "brave"
  "firefox" "slack" "discord" "postman" "goose" "figma"
)
LOCK_ROOTS=(
  "$HOME/.cache/JetBrains
$HOME/.config/JetBrains
$HOME/.local/share/JetBrains"
  "$HOME/.cache/JetBrains
$HOME/.config/JetBrains
$HOME/.local/share/JetBrains"
  "$HOME/.local/share/zed
$HOME/.cache/zed"
  "$HOME/.config/Code
$HOME/.config/Cursor
$HOME/.cache/Code"
  "$HOME/.cache/google-chrome
$HOME/.config/google-chrome
$HOME/.cache/Google"
  "$HOME/.cache/BraveSoftware
$HOME/.config/BraveSoftware"
  "$HOME/.cache/mozilla
$HOME/.mozilla"
  "$HOME/.config/Slack"
  "$HOME/.config/discord"
  "$HOME/.config/Postman"
  "$HOME/.config/goose"
  "$HOME/.config/figma-linux"
)

# Overridable by self-test so lock logic can be exercised without real processes.
ST_FAKE_PROCS=""

running_procs() {
  if [[ -n "$ST_FAKE_PROCS" ]]; then printf '%s\n' "$ST_FAKE_PROCS"; return 0; fi
  ps -eo pid=,args= 2>/dev/null |
    grep -vE 'cleanup-ubuntu|[[:space:]]grep[[:space:]]' |
    sed 's/^[[:space:]]*//; s/[[:space:]]\{1,\}/\t/'
}

# True if a cmdline matches any lock regex. Exposed for self-test.
lock_rx_matches() {
  local cmd="$1" rx
  for rx in "${LOCK_RX[@]}"; do
    [[ "$cmd" =~ cleanup-ubuntu ]] && return 1
    if printf '%s' "$cmd" | grep -qE "$rx"; then return 0; fi
  done
  return 1
}

# Fill U_LOCKED[id] with "AppName:pid" for every unit whose paths sit under a
# root owned by a running app.
apply_locks() {
  local -a procs=()
  readarray -t procs < <(running_procs)
  (( ${#procs[@]} )) || return 0

  local id i rx pid cmd root p
  local -a roots=() plist=()

  for i in "${!LOCK_RX[@]}"; do
    rx="${LOCK_RX[$i]}"
    pid=""
    for entry in "${procs[@]}"; do
      [[ -z "$entry" ]] && continue
      cmd="${entry#*$'\t'}"
      [[ "$cmd" =~ cleanup-ubuntu ]] && continue
      if printf '%s' "$cmd" | grep -qE "$rx"; then pid="${entry%%$'\t'*}"; break; fi
    done
    [[ -z "$pid" ]] && continue

    readarray -t roots <<< "${LOCK_ROOTS[$i]}"
    for id in "${U_IDS[@]}"; do
      [[ -n "${U_LOCKED[$id]}" ]] && continue
      [[ "${U_KIND[$id]}" == cmd ]] && continue
      readarray -t plist < <(unit_paths "$id")
      for p in "${plist[@]}"; do
        [[ -z "$p" ]] && continue
        for root in "${roots[@]}"; do
          [[ -z "$root" ]] && continue
          if [[ "$p" == "$root" || "$p" == "$root"/* ]]; then
            U_LOCKED[$id]="${LOCK_NAME[$i]}:$pid"
            break 3
          fi
        done
      done
    done
  done
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | tail -3`

Expected: `failed 0`.

Then verify against reality — with Android Studio open:

```bash
pgrep -af 'android-studio' | head -1
```

Confirm the cmdline printed is one `lock_rx_matches` accepts.

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "fix: detect android-studio as a JetBrains cache lock holder

The --jetbrains guard matched on product names only, so a running
Android Studio (/usr/local/android-studio/bin/studio) passed the check
and its live indexes were eligible for deletion. Replaces the single
regex with a table mapping process patterns to the cache roots they own,
covering studio, zed, vscode, cursor, slack, discord, postman and goose."
```

---

### Task 5: Port existing cleanup steps to units

Converts the current sections 1–11 into registered units. Behaviour is unchanged; only the plumbing moves. The old `reclaim()`, `run_clean()`, `reclaim_stale()` and `is_running()` are deleted at the end of this task.

**Files:**
- Modify: `cleanup-ubuntu.sh` — replace lines 194–557 (sections 1 through 11) with `register_all_units()`; delete `reclaim()` (133–148), `run_clean()` (151–159), `reclaim_stale()` (163–170), `is_running()` (127)

**Interfaces:**
- Consumes: `register_unit`
- Produces: `register_all_units` — registers every built-in unit with its tier, reversibility and legacy flag

- [ ] **Step 1: Write the failing test**

```bash
sttest_builtin_units() {
  U_IDS=(); unset U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED
  declare -gA U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED
  register_all_units

  st_assert_ne "units were registered" "${#U_IDS[@]}" "0"

  # tier assignment matches the spec
  st_assert "npm cache is tier 1"        "${U_TIER[npm-cacache]}"      "1"
  st_assert "playwright is tier 2"       "${U_TIER[playwright]}"       "2"
  st_assert "jetbrains cache is tier 3"  "${U_TIER[jetbrains-cache]}"  "3"
  st_assert "claude history is tier 4"   "${U_TIER[claude-history]}"   "4"
  st_assert "docker volumes are tier 5"  "${U_TIER[docker-volumes]}"   "5"

  # reversibility must track the ceiling exactly
  local id bad=0
  for id in "${U_IDS[@]}"; do
    if (( ${U_TIER[$id]} <= 3 )) && [[ "${U_REV[$id]}" != 1 ]]; then bad=1; fi
    if (( ${U_TIER[$id]} >= 4 )) && [[ "${U_REV[$id]}" != 0 ]]; then bad=1; fi
  done
  st_assert "reversible flag matches tier for every unit" "$bad" "0"

  # legacy flags stay wired to their unit
  st_assert "playwright keeps its flag"  "${U_FLAG[playwright]}"       "--playwright"
  st_assert "jetbrains keeps its flag"   "${U_FLAG[jetbrains-cache]}"  "--jetbrains"
  st_assert "claude-history keeps flag"  "${U_FLAG[claude-history]}"   "--claude-history"

  # no duplicate ids
  local dupes
  dupes=$(printf '%s\n' "${U_IDS[@]}" | sort | uniq -d | wc -l)
  st_assert "no duplicate unit ids" "$dupes" "0"
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | grep -A3 builtin_units | head -20`

Expected: FAIL with `register_all_units: command not found`.

- [ ] **Step 3: Write minimal implementation**

Replace lines 194–557 with:

```bash
# ---------------------------------------------------------------------------------
# built-in unit definitions
#
# tier 0 free | 1-3 regenerable (auto-escalation may select these)
# ================= ceiling =================
# tier 4-5 lossy (requires --allow-lossy)
# ---------------------------------------------------------------------------------
register_all_units() {
  # --- tier 0: native cache cleaners, cost nothing, delete nothing precious ---
  command -v npm      >/dev/null 2>&1 && register_unit npm-native      0 1 "npm cache clean"    cmd "npm cache clean --force"
  command -v pnpm     >/dev/null 2>&1 && register_unit pnpm-native     0 1 "pnpm store prune"   cmd "pnpm store prune"
  command -v yarn     >/dev/null 2>&1 && register_unit yarn-native     0 1 "yarn cache clean"   cmd "yarn cache clean"
  command -v bun      >/dev/null 2>&1 && register_unit bun-native      0 1 "bun cache rm"       cmd "bun pm cache rm"
  command -v go       >/dev/null 2>&1 && register_unit go-native       0 1 "go clean -cache"    cmd "go clean -cache -modcache"
  command -v composer >/dev/null 2>&1 && register_unit composer-native 0 1 "composer clear"     cmd "composer clear-cache"
  command -v pip      >/dev/null 2>&1 && register_unit pip-native      0 1 "pip cache purge"    cmd "pip cache purge"
  command -v uv       >/dev/null 2>&1 && register_unit uv-native       0 1 "uv cache clean"     cmd "uv cache clean"

  register_unit thumbnails 0 1 "thumbnails" paths "$HOME/.cache/thumbnails"
  register_unit trash      0 1 "Trash"      paths "$HOME/.local/share/Trash/files
$HOME/.local/share/Trash/info"

  # --- tier 1: package-manager cache leftovers ---
  register_unit npm-cacache   1 1 "npm _cacache"       paths "$HOME/.npm/_cacache"
  register_unit npm-npx       1 1 "npm _npx"           paths "$HOME/.npm/_npx"
  register_unit npm-logs      1 1 "npm _logs"          paths "$HOME/.npm/_logs"
  register_unit yarn-classic  1 1 "yarn (classic)"     paths "$HOME/.cache/yarn"
  register_unit yarn-berry    1 1 "yarn berry cache"   paths "$HOME/.yarn/berry/cache"
  register_unit pnpm-cache    1 1 "pnpm cache"         paths "$HOME/.cache/pnpm"
  register_unit pnpm-store    1 1 "pnpm store"         paths "$HOME/.local/share/pnpm/store"
  register_unit bun-cache     1 1 "bun cache"          paths "$HOME/.bun/install/cache"
  register_unit node-gyp      1 1 "node-gyp headers"   paths "$HOME/.cache/node-gyp"
  register_unit go-build      1 1 "go build cache"     paths "$HOME/.cache/go-build"
  register_unit go-modcache   1 1 "go module cache"    paths "$HOME/go/pkg/mod"
  register_unit composer-c    1 1 "composer cache"     paths "$HOME/.cache/composer"
  register_unit uv-cache      1 1 "uv cache"           paths "$HOME/.cache/uv"
  register_unit pip-cache     1 1 "pip cache"          paths "$HOME/.cache/pip"
  register_unit cargo-cache   1 1 "cargo registry"     paths "$HOME/.cargo/registry/cache
$HOME/.cargo/registry/src"
  register_unit phpactor      1 1 "phpactor index"     paths "$HOME/.cache/phpactor"
  register_unit devtools-mcp  1 1 "chrome-devtools-mcp" paths "$HOME/.cache/chrome-devtools-mcp"
  register_unit act-cache     1 1 "act (gh actions)"   paths "$HOME/.cache/act"
  register_unit giget         1 1 "giget templates"    paths "$HOME/.cache/giget"

  # --- tier 2: bigger regenerable artefacts ---
  register_unit playwright 2 1 "playwright browsers" paths "$HOME/.cache/ms-playwright" "--playwright"
  register_claude_job_units
  register_claude_plugin_units

  # --- tier 3: costs a reindex or a cold load ---
  register_unit jetbrains-cache 3 1 "JetBrains caches" paths "$HOME/.cache/JetBrains" "--jetbrains"
  register_browser_units

  # ================= auto-escalation ceiling =================

  # --- tier 4: loses information ---
  register_claude_history_units
  register_dep_units

  # --- tier 5: loses information, possibly irreplaceable ---
  register_unit claude-vm      5 0 "Claude VM bundles" paths "$HOME/.config/Claude/vm_bundles" "--claude-vm"
  register_unit docker-volumes 5 0 "docker volumes"    cmd   "docker volume prune -f" "--docker-volumes"
  register_unit docker-prune   5 0 "docker prune"      cmd   "docker builder prune -f; docker container prune -f; docker network prune -f; docker image prune -f" "--docker"
}
```

Then port the four steps whose path lists are computed rather than literal. Each keeps the exact selection logic the current script uses — only the deletion is deferred to `run_unit`.

```bash
# Finished claude jobs. Same three guards as before: never this script's own job,
# never one written to in the last hour, never a non-terminal state.
register_claude_job_units() {
  local jobs_root="$CLAUDE_ROOT/jobs"
  [[ -d "$jobs_root" ]] || return 0
  local self_job jd jid jstate age_days now_ts
  self_job=$(basename "${CLAUDE_JOB_DIR:-/nonexistent}")
  now_ts=$(date +%s)

  for jd in "$jobs_root"/*/; do
    jd=${jd%/}; [[ -d "$jd" ]] || continue
    jid=$(basename "$jd")
    [[ "$jid" == "$self_job" ]] && continue
    [[ -n "$(find "$jd" -maxdepth 1 -mmin -60 2>/dev/null)" ]] && continue

    jstate=none
    if [[ -f "$jd/state.json" ]]; then
      jstate=$(grep -oE '"state"[[:space:]]*:[[:space:]]*"[^"]+"' "$jd/state.json" |
                 head -1 | sed 's/.*"\([^"]*\)"$/\1/')
      jstate=${jstate:-unknown}
    fi
    case "$jstate" in done|failed|none) ;; *) continue ;; esac

    age_days=$(( (now_ts - $(stat -c %Y "$jd" 2>/dev/null || echo "$now_ts")) / 86400 ))
    if (( age_days >= CLAUDE_JOBS_DAYS )); then
      register_unit "claude-job-$jid" 2 1 "job $jid ($jstate, ${age_days}d)" paths "$jd" "--claude-jobs"
    elif [[ -d "$jd/tmp" ]]; then
      register_unit "claude-job-$jid" 2 1 "job $jid scratch ($jstate, ${age_days}d)" paths "$jd/tmp" "--claude-jobs"
    fi
  done
}

# Superseded plugin versions. Strictly version-shaped names only, so a
# "1.3.0.bak-pre-sync" directory can never be mistaken for the newest release.
register_claude_plugin_units() {
  local plugin_cache="$CLAUDE_ROOT/plugins/cache" pdir pname newest v
  [[ -d "$plugin_cache" ]] || return 0
  local -a vers=()
  for pdir in "$plugin_cache"/*/*/; do
    pdir=${pdir%/}; [[ -d "$pdir" ]] || continue
    readarray -t vers < <(find "$pdir" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' 2>/dev/null |
                            grep -E '^v?[0-9]+(\.[0-9]+)+$' | sort -V)
    (( ${#vers[@]} > 1 )) || continue
    newest="${vers[${#vers[@]}-1]}"
    pname="$(basename "$(dirname "$pdir")")/$(basename "$pdir")"
    for v in "${vers[@]}"; do
      [[ "$v" == "$newest" ]] && continue
      register_unit "claude-plugin-${pname//\//-}-$v" 2 1 "$pname $v" paths "$pdir/$v" "--claude-plugins"
    done
  done
}

# Browser HTTP caches. Profile data (history, passwords, Local Storage) is never
# in these paths.
register_browser_units() {
  local root
  root="$HOME/.config/google-chrome"
  [[ -d "$root" ]] && register_unit chrome-profile-cache 3 1 "Chrome profile caches" paths \
    "$(printf '%s\n' "$root"/*/Cache "$root"/*/"Code Cache" "$root"/*/GPUCache 2>/dev/null)" "--browsers"
  register_unit chrome-cache 3 1 "chrome ~/.cache" paths "$HOME/.cache/google-chrome
$HOME/.cache/Google" "--browsers"
  root="$HOME/.config/BraveSoftware/Brave-Browser"
  [[ -d "$root" ]] && register_unit brave-profile-cache 3 1 "Brave profile caches" paths \
    "$(printf '%s\n' "$root"/*/Cache "$root"/*/"Code Cache" "$root"/*/GPUCache 2>/dev/null)" "--browsers"
  register_unit brave-cache   3 1 "brave ~/.cache"   paths "$HOME/.cache/BraveSoftware" "--browsers"
  register_unit firefox-cache 3 1 "firefox ~/.cache" paths "$HOME/.cache/mozilla" "--browsers"
}

# LOSSY. Removes `claude --resume` for those sessions. memory/ is never touched.
register_claude_history_units() {
  local days=${CLAUDE_HISTORY_DAYS:-30}
  local -a old=()
  readarray -t old < <(find "$CLAUDE_ROOT/projects" -mindepth 2 -maxdepth 2 -type f \
                         -name '*.jsonl' -mtime "+$days" 2>/dev/null)
  (( ${#old[@]} )) && register_unit claude-history 4 0 "session transcripts (${#old[@]})" paths \
    "$(printf '%s\n' "${old[@]}")" "--claude-history"

  local d
  for d in file-history shell-snapshots session-env paste-cache; do
    local -a hits=()
    readarray -t hits < <(find "$CLAUDE_ROOT/$d" -mindepth 1 -maxdepth 1 -mtime "+$days" 2>/dev/null)
    (( ${#hits[@]} )) && register_unit "claude-$d" 4 0 "$d (${#hits[@]})" paths \
      "$(printf '%s\n' "${hits[@]}")" "--claude-history"
  done
}

# LOSSY. A dependency dir is only registered when its manifest sits beside it,
# so an unrelated directory named "vendor" is never touched.
register_dep_units() {
  local days=${DEPS_DAYS:-0}
  (( days > 0 )) || return 0
  local -a roots=() ; local r nm vd
  for r in "$HOME/Projects" "$HOME/Sites" "$HOME/code" "$HOME/dev"; do
    [[ -d "$r" ]] && roots+=("$r")
  done
  (( ${#roots[@]} )) || return 0

  while IFS= read -r nm; do
    [[ -z "$nm" ]] && continue
    [[ -f "$(dirname "$nm")/package.json" ]] || continue
    register_unit "deps-nm-${nm//\//-}" 4 0 "nm  $(dirname "$nm" | sed "s#$HOME#~#")" paths "$nm" "--deps"
  done < <(find "${roots[@]}" -maxdepth 6 -type d -name node_modules -prune -mtime "+$days" 2>/dev/null)

  while IFS= read -r vd; do
    [[ -z "$vd" ]] && continue
    [[ -f "$(dirname "$vd")/composer.json" ]] || continue
    register_unit "deps-vendor-${vd//\//-}" 4 0 "vendor  $(dirname "$vd" | sed "s#$HOME#~#")" paths "$vd" "--deps"
  done < <(find "${roots[@]}" -maxdepth 6 -type d -name vendor -prune -mtime "+$days" 2>/dev/null)
}
```

Now delete the superseded helpers: `reclaim()`, `run_clean()`, `reclaim_stale()`, `is_running()`.

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | tail -3`

Expected: `failed 0`.

Then verify no dead references remain:

```bash
grep -nE '\b(reclaim|run_clean|reclaim_stale|is_running)\b' cleanup-ubuntu.sh
```

Expected: no output.

And confirm bash still parses the whole file: `bash -n cleanup-ubuntu.sh && echo "syntax ok"`

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "refactor: port all cleanup steps to the unit registry"
```

---

### Task 6: Discover generic app caches

**Files:**
- Modify: `cleanup-ubuntu.sh` — insert after the built-in unit definitions

**Interfaces:**
- Consumes: `register_unit`
- Produces: `CACHE_NAMES` and `PROTECTED_NAMES` arrays; `is_cache_name <name>` → exit 0/1; `discover_app_caches` → registers matching dirs as tier-2 units with id prefix `disc-`

- [ ] **Step 1: Write the failing test**

```bash
sttest_discovery_caches() {
  # cache-semantic names are matched
  st_assert "Cache matched"        "$(is_cache_name 'Cache' && echo y || echo n)"        "y"
  st_assert "Code Cache matched"   "$(is_cache_name 'Code Cache' && echo y || echo n)"   "y"
  st_assert "GPUCache matched"     "$(is_cache_name 'GPUCache' && echo y || echo n)"     "y"
  st_assert "CachedData matched"   "$(is_cache_name 'CachedData' && echo y || echo n)"   "y"
  st_assert "ShaderCache matched"  "$(is_cache_name 'ShaderCache' && echo y || echo n)"  "y"
  st_assert "Crashpad matched"     "$(is_cache_name 'Crashpad' && echo y || echo n)"     "y"

  # PROTECTED — these hold real application state and must never match
  st_assert "Local Storage PROTECTED"  "$(is_cache_name 'Local Storage' && echo y || echo n)"  "n"
  st_assert "IndexedDB PROTECTED"      "$(is_cache_name 'IndexedDB' && echo y || echo n)"      "n"
  st_assert "Session Storage PROTECTED" "$(is_cache_name 'Session Storage' && echo y || echo n)" "n"
  st_assert "databases PROTECTED"      "$(is_cache_name 'databases' && echo y || echo n)"      "n"
  st_assert "Service Worker PROTECTED" "$(is_cache_name 'Service Worker' && echo y || echo n)" "n"
  st_assert "Local State PROTECTED"    "$(is_cache_name 'Local State' && echo y || echo n)"    "n"
  st_assert "Preferences PROTECTED"    "$(is_cache_name 'Preferences' && echo y || echo n)"    "n"
  st_assert "unrelated name unmatched" "$(is_cache_name 'Extensions' && echo y || echo n)"     "n"

  # end-to-end against the fixture
  local root; root=$(st_fixture)
  U_IDS=(); unset U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED
  declare -gA U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED

  DISCOVER_ROOTS=("$root/.config" "$root/.local/share" "$root/.cache")
  discover_app_caches

  local found_cache=0 found_protected=0 id
  for id in "${U_IDS[@]}"; do
    [[ "${U_PAYLOAD[$id]}" == *"FakeApp/Cache" ]]           && found_cache=1
    [[ "${U_PAYLOAD[$id]}" == *"Local Storage"* ]]          && found_protected=1
    [[ "${U_PAYLOAD[$id]}" == *"IndexedDB"* ]]              && found_protected=1
  done
  st_assert "discovered FakeApp/Cache"          "$found_cache"     "1"
  st_assert "NEVER discovered protected dirs"   "$found_protected" "0"

  for id in "${U_IDS[@]}"; do
    st_assert "discovered unit $id is tier 2" "${U_TIER[$id]}" "2"
  done
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | grep -A3 discovery_caches | head -20`

Expected: FAIL with `is_cache_name: command not found`.

- [ ] **Step 3: Write minimal implementation**

```bash
# ---------------------------------------------------------------------------------
# discovery: generic app caches
#
# Matches on directory NAME, not on any app-specific knowledge, so apps the
# script has never heard of still get cleaned. PROTECTED_NAMES is the only thing
# standing between this and real application state — treat it as security
# critical and keep sttest_discovery_caches green.
# ---------------------------------------------------------------------------------
CACHE_NAMES=(
  "Cache" "Code Cache" "GPUCache" "ShaderCache" "CachedData"
  "DawnCache" "DawnGraphiteCache" "GrShaderCache" "Crashpad"
  "Cache_Data" "component_crx_cache" "logs"
)
PROTECTED_NAMES=(
  "Local Storage" "IndexedDB" "Session Storage" "databases"
  "Service Worker" "Local State" "Preferences" "Cookies"
  "Login Data" "Web Data" "History" "Bookmarks"
)
DISCOVER_ROOTS=("$HOME/.config" "$HOME/.local/share" "$HOME/.cache")

is_cache_name() {
  local n="$1" x
  for x in "${PROTECTED_NAMES[@]}"; do [[ "$n" == "$x" ]] && return 1; done
  for x in "${CACHE_NAMES[@]}";     do [[ "$n" == "$x" ]] && return 0; done
  return 1
}

discover_app_caches() {
  local r d name app id
  for r in "${DISCOVER_ROOTS[@]}"; do
    [[ -d "$r" ]] || continue
    while IFS= read -r d; do
      [[ -z "$d" ]] && continue
      name=$(basename "$d")
      is_cache_name "$name" || continue
      app=$(basename "$(dirname "$d")")
      id="disc-${app}-${name}"
      id="${id// /-}"
      [[ -n "${U_TIER[$id]:-}" ]] && continue
      register_unit "$id" 2 1 "$app/$name" paths "$d"
    done < <(find "$r" -mindepth 2 -maxdepth 2 -type d 2>/dev/null)
  done
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | tail -3`

Expected: `failed 0`. The four `PROTECTED` assertions are the ones that matter — a regression there means the script can delete browser logins.

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "feat: discover generic app caches by directory name

Pattern-matches cache-semantic subdirectory names under ~/.config,
~/.local/share and ~/.cache, so apps with no hardcoded rule (Slack,
Postman, zed, goose, discord) are covered. A protected-name list keeps
Local Storage, IndexedDB, Cookies and friends permanently out of scope."
```

---

### Task 7: Discover report-only heavyweights

**Files:**
- Modify: `cleanup-ubuntu.sh` — insert after `discover_app_caches`

**Interfaces:**
- Consumes: `path_bytes`, `U_PAYLOAD`
- Produces: `HEAVY_LIST` array of `bytes<TAB>path<TAB>class` rows; `classify_dir <path>` → `cache|data|mixed`; `discover_heavyweights` → fills `HEAVY_LIST`. Nothing here is ever deleted.

- [ ] **Step 1: Write the failing test**

```bash
sttest_heavyweights() {
  local root; root=$(st_fixture)
  mkdir -p "$root/BigData"
  truncate -s 300M "$root/BigData/blob.bin"
  mkdir -p "$root/SmallThing"
  truncate -s 1M   "$root/SmallThing/blob.bin"

  U_IDS=(); unset U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED
  declare -gA U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED
  HEAVY_LIST=()
  HEAVY_ROOTS=("$root")
  HEAVY_MIN_BYTES=$(( 200 * 1024 * 1024 ))
  discover_heavyweights

  local has_big=0 has_small=0 row
  for row in "${HEAVY_LIST[@]}"; do
    [[ "$row" == *"BigData"* ]]    && has_big=1
    [[ "$row" == *"SmallThing"* ]] && has_small=1
  done
  st_assert "heavyweight over threshold listed"  "$has_big"   "1"
  st_assert "dir under threshold not listed"     "$has_small" "0"

  # report-only: the bytes must still be on disk afterwards
  st_assert "heavyweight NOT deleted" \
    "$([[ -e "$root/BigData/blob.bin" ]] && echo yes || echo no)" "yes"

  # a dir already claimed by a unit must not be double-reported
  HEAVY_LIST=()
  register_unit "claims-big" 2 1 "claims big" paths "$root/BigData"
  discover_heavyweights
  local dupe=0
  for row in "${HEAVY_LIST[@]}"; do [[ "$row" == *"BigData"* ]] && dupe=1; done
  st_assert "claimed dir excluded from report" "$dupe" "0"

  st_assert "classify_dir names a class" \
    "$(classify_dir "$root/BigData" | grep -cE '^(cache|data|mixed)$')" "1"
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | grep -A3 heavyweights | head -20`

Expected: FAIL with `discover_heavyweights: command not found`.

- [ ] **Step 3: Write minimal implementation**

```bash
# ---------------------------------------------------------------------------------
# discovery: report-only heavyweights
#
# Anything big that no unit claims. Never deleted — this exists so the user can
# see where the space actually went (e.g. ~/Downloads at 5.5G) and decide for
# themselves.
# ---------------------------------------------------------------------------------
HEAVY_LIST=()
HEAVY_ROOTS=("$HOME")
HEAVY_MIN_BYTES=$(( 200 * 1024 * 1024 ))

# Guess what a directory holds, so the report can hint without deciding.
classify_dir() {
  local d="$1" n
  n=$(basename "$d")
  case "$n" in
    .cache|cache|Cache|*Cache) echo cache; return ;;
    Downloads|Documents|Pictures|Videos|Music) echo data; return ;;
  esac
  if find "$d" -maxdepth 2 -type d \( -name Cache -o -name 'Code Cache' -o -name GPUCache \) \
       -print -quit 2>/dev/null | grep -q .; then
    echo mixed
  else
    echo data
  fi
}

# True if any registered unit already covers this path.
claimed_by_unit() {
  local d="$1" id p
  local -a plist=()
  for id in "${U_IDS[@]}"; do
    [[ "${U_KIND[$id]}" == cmd ]] && continue
    readarray -t plist < <(unit_paths "$id")
    for p in "${plist[@]}"; do
      [[ -z "$p" ]] && continue
      [[ "$d" == "$p" || "$d" == "$p"/* || "$p" == "$d"/* ]] && return 0
    done
  done
  return 1
}

discover_heavyweights() {
  local r d b cls
  for r in "${HEAVY_ROOTS[@]}"; do
    [[ -d "$r" ]] || continue
    while IFS= read -r d; do
      [[ -z "$d" ]] && continue
      b=$(path_bytes "$d")
      (( b >= HEAVY_MIN_BYTES )) || continue
      claimed_by_unit "$d" && continue
      cls=$(classify_dir "$d")
      HEAVY_LIST+=("$b"$'\t'"$d"$'\t'"$cls")
    done < <(find "$r" -mindepth 1 -maxdepth 1 -type d 2>/dev/null)
  done
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | tail -3`

Expected: `failed 0`.

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "feat: report large unclaimed directories without deleting them"
```

---

### Task 8: Planner — target resolution, selection, tier ceiling

**Files:**
- Modify: `cleanup-ubuntu.sh` — insert after discovery

**Interfaces:**
- Consumes: `parse_size`, `avail_bytes`, `pressure_of`, `mount_of`, `U_*` arrays
- Produces: `TARGET_BYTES` (empty when no target), `TARGET_PATH`, `TARGET_MOUNT`; `resolve_target`; `select_units` → fills `SELECTED` array in execution order; `SKIPPED_LOSSY` array of ids withheld by the ceiling

- [ ] **Step 1: Write the failing test**

```bash
sttest_planner() {
  local root; root=$(st_fixture)
  U_IDS=(); unset U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED
  declare -gA U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED

  register_unit t1a 1 1 "t1a" paths "$root/.cache/npm/blob"
  register_unit t3a 3 1 "t3a" paths "$root/.config/FakeApp/Cache"
  register_unit t4a 4 0 "t4a" paths "$root/.config/FakeApp/Local Storage"
  register_unit t5a 5 0 "t5a" paths "$root/.config/FakeApp/IndexedDB"
  local id; for id in "${U_IDS[@]}"; do probe_unit "$id"; done

  TARGET_MOUNT=$(mount_of "$root")
  for id in "${U_IDS[@]}"; do U_MOUNT[$id]="$TARGET_MOUNT"; done

  # ceiling holds by default
  ALLOW_LOSSY=0; TIER_CAP=5; FORCED_FLAGS=""
  select_units
  st_assert "tier 1 selected"      "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t1a$')" "1"
  st_assert "tier 3 selected"      "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t3a$')" "1"
  st_assert "TIER 4 WITHHELD"      "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t4a$')" "0"
  st_assert "TIER 5 WITHHELD"      "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t5a$')" "0"
  st_assert "withheld are reported" "$(printf '%s\n' "${SKIPPED_LOSSY[@]}" | grep -c '^t4a$')" "1"

  # ordering: lower tier first
  st_assert "tier order respected" "${SELECTED[0]}" "t1a"

  # --allow-lossy opens the gate
  ALLOW_LOSSY=1
  select_units
  st_assert "allow-lossy admits tier 4" "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t4a$')" "1"
  st_assert "allow-lossy admits tier 5" "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t5a$')" "1"

  # --tier caps below the ceiling
  ALLOW_LOSSY=0; TIER_CAP=1
  select_units
  st_assert "tier cap admits t1a"  "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t1a$')" "1"
  st_assert "tier cap excludes t3a" "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t3a$')" "0"
  TIER_CAP=5

  # units on another mount are excluded
  U_MOUNT[t3a]="/some/other/mount"
  select_units
  st_assert "off-target mount excluded" "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t3a$')" "0"
  U_MOUNT[t3a]="$TARGET_MOUNT"

  # locked units are excluded
  U_LOCKED[t3a]="fakeapp:999"
  select_units
  st_assert "locked unit excluded" "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t3a$')" "0"
  U_LOCKED[t3a]=""

  # a legacy flag force-includes past the tier cap, but NOT past the ceiling
  TIER_CAP=1; U_FLAG[t3a]="--jetbrains"; FORCED_FLAGS="--jetbrains"
  select_units
  st_assert "legacy flag forces inclusion" "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t3a$')" "1"
  U_FLAG[t4a]="--claude-history"; FORCED_FLAGS="--jetbrains --claude-history"
  select_units
  st_assert "legacy flag alone cannot cross ceiling" \
    "$(printf '%s\n' "${SELECTED[@]}" | grep -c '^t4a$')" "0"
  TIER_CAP=5; FORCED_FLAGS=""

  # target parsing
  TARGET_BYTES=""; FREE_ARG="12G"; TARGET_PATH="$HOME"
  resolve_target
  st_assert "resolve_target parses --free" "$TARGET_BYTES" "12884901888"
  FREE_ARG="12G:$root"
  resolve_target
  st_assert "resolve_target honours :path" "$TARGET_PATH" "$root"
  FREE_ARG=""
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | grep -A3 planner | head -20`

Expected: FAIL with `select_units: command not found`.

- [ ] **Step 3: Write minimal implementation**

```bash
# ---------------------------------------------------------------------------------
# planner
# ---------------------------------------------------------------------------------
TARGET_BYTES="" TARGET_PATH="$HOME" TARGET_MOUNT=""
SELECTED=() SKIPPED_LOSSY=()

# --free 12G | --free 12G:/mnt/data | --auto (reach <85% on the worst mount)
resolve_target() {
  local pct_goal=85
  if [[ -n "${FREE_ARG:-}" ]]; then
    local sz="${FREE_ARG%%:*}" pth="${FREE_ARG#*:}"
    [[ "$pth" == "$FREE_ARG" ]] && pth="$HOME"
    TARGET_PATH="$pth"
    TARGET_BYTES=$(parse_size "$sz") || {
      printf '%sbad --free size: %s%s (use e.g. 12G)\n' "$C_RED" "$sz" "$C_RESET" >&2
      exit 2
    }
  elif [[ ${AUTO_MODE:-0} -eq 1 ]]; then
    TARGET_PATH="$HOME"
    local total_kb
    total_kb=$(df -P "$TARGET_PATH" 2>/dev/null | awk 'NR==2{print $2}')
    TARGET_BYTES=$(( total_kb * 1024 * (100 - pct_goal) / 100 ))
  else
    TARGET_BYTES=""
  fi
  TARGET_MOUNT=$(mount_of "$TARGET_PATH")
}

target_met() {
  [[ -z "$TARGET_BYTES" ]] && return 1
  (( $(avail_bytes "$TARGET_PATH") >= TARGET_BYTES ))
}

# Fill SELECTED in execution order: tier ascending, then bytes descending so the
# cheapest big wins land first within a tier.
select_units() {
  SELECTED=() SKIPPED_LOSSY=()
  local id forced
  local -a rows=()

  for id in "${U_IDS[@]}"; do
    forced=0
    if [[ -n "${U_FLAG[$id]}" && " ${FORCED_FLAGS:-} " == *" ${U_FLAG[$id]} "* ]]; then forced=1; fi

    # the ceiling is absolute — a legacy flag cannot cross it, only --allow-lossy can
    if [[ "${U_REV[$id]}" != 1 ]] && [[ ${ALLOW_LOSSY:-0} -ne 1 ]]; then
      SKIPPED_LOSSY+=("$id"); continue
    fi
    (( ${U_TIER[$id]} > ${TIER_CAP:-5} )) && (( forced == 0 )) && continue
    [[ -n "${U_LOCKED[$id]}" ]] && continue
    if [[ -n "$TARGET_MOUNT" && -n "${U_MOUNT[$id]}" && "${U_MOUNT[$id]}" != "$TARGET_MOUNT" ]]; then
      (( forced == 0 )) && continue
    fi
    rows+=("$(printf '%d %012d %s' "${U_TIER[$id]}" "${U_BYTES[$id]}" "$id")")
  done

  readarray -t rows < <(printf '%s\n' "${rows[@]}" | sort -k1,1n -k2,2nr)
  for id in "${rows[@]}"; do
    [[ -z "$id" ]] && continue
    SELECTED+=("${id##* }")
  done
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | tail -3`

Expected: `failed 0`. `TIER 4 WITHHELD`, `TIER 5 WITHHELD` and `legacy flag alone cannot cross ceiling` are the safety-critical ones.

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "feat: add planner with target resolution and tier ceiling"
```

---

### Task 9: Executor with early stop

**Files:**
- Modify: `cleanup-ubuntu.sh` — insert after the planner

**Interfaces:**
- Consumes: `SELECTED`, `run_unit`, `target_met`, `avail_bytes`, `confirm`
- Produces: `execute_plan` → runs selected units, sets `TOTAL_BYTES`, `STOPPED_EARLY`, `UNRUN` (ids never reached)

- [ ] **Step 1: Write the failing test**

```bash
sttest_executor() {
  local root; root=$(st_fixture)
  U_IDS=(); unset U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED
  declare -gA U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED

  register_unit e1 1 1 "e1" paths "$root/.cache/npm/blob"
  register_unit e2 2 1 "e2" paths "$root/.config/FakeApp/Cache"
  local id; for id in "${U_IDS[@]}"; do probe_unit "$id"; done
  SELECTED=(e1 e2)

  # dry-run must delete nothing
  APPLY=0 ASSUME_YES=1 TARGET_BYTES="" TOTAL_BYTES=0
  execute_plan
  st_assert "dry-run deletes nothing (e1)" "$([[ -e "$root/.cache/npm/blob" ]] && echo yes)" "yes"
  st_assert "dry-run deletes nothing (e2)" "$([[ -e "$root/.config/FakeApp/Cache" ]] && echo yes)" "yes"
  st_assert "dry-run still totals bytes"   "$TOTAL_BYTES" "3145728"

  # apply with an already-satisfied target must run nothing at all
  APPLY=1 TARGET_BYTES=1 TARGET_PATH="$root" TOTAL_BYTES=0
  execute_plan
  st_assert "target already met - nothing ran" "$TOTAL_BYTES" "0"
  st_assert "early stop recorded"              "$STOPPED_EARLY" "1"
  st_assert "files survive early stop"         "$([[ -e "$root/.cache/npm/blob" ]] && echo yes)" "yes"

  # apply with an unreachable target must run everything
  APPLY=1 TARGET_BYTES=$(( 1024 ** 5 )) TOTAL_BYTES=0
  execute_plan
  st_assert "unreachable target runs all"  "$TOTAL_BYTES" "3145728"
  st_assert "e1 deleted" "$([[ -e "$root/.cache/npm/blob" ]] && echo yes || echo no)" "no"
  st_assert "e2 deleted" "$([[ -e "$root/.config/FakeApp/Cache" ]] && echo yes || echo no)" "no"
  st_assert "protected fixture dir untouched" \
    "$([[ -e "$root/.config/FakeApp/Local Storage" ]] && echo yes)" "yes"
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | grep -A3 executor | head -20`

Expected: FAIL with `execute_plan: command not found`.

- [ ] **Step 3: Write minimal implementation**

```bash
# ---------------------------------------------------------------------------------
# executor
# ---------------------------------------------------------------------------------
STOPPED_EARLY=0
UNRUN=()

execute_plan() {
  STOPPED_EARLY=0; UNRUN=()
  local id freed i=0

  for id in "${SELECTED[@]}"; do
    i=$((i + 1))
    if [[ $APPLY -eq 1 ]] && target_met; then
      STOPPED_EARLY=1
      UNRUN=("${SELECTED[@]:$((i - 1))}")
      break
    fi

    if [[ ${U_BYTES[$id]} -eq 0 && "${U_KIND[$id]}" != cmd ]]; then continue; fi

    if [[ $APPLY -eq 1 ]]; then
      freed=$(run_unit "$id")
      TOTAL_BYTES=$(( TOTAL_BYTES + freed ))
      printf '  %s✓%s %-38s %s%s%s freed\n' "$C_GRN" "$C_RESET" \
        "${U_LABEL[$id]}" "$C_GRN" "$(human "$freed")" "$C_RESET"
    else
      TOTAL_BYTES=$(( TOTAL_BYTES + ${U_BYTES[$id]} ))
      if [[ "${U_KIND[$id]}" == cmd ]]; then
        printf '  %s•%s %-38s would run: %s\n' "$C_CYN" "$C_RESET" "${U_LABEL[$id]}" "${U_PAYLOAD[$id]}"
      else
        printf '  %s•%s %-38s %s%s%s reclaimable\n' "$C_CYN" "$C_RESET" \
          "${U_LABEL[$id]}" "$C_B" "$(human "${U_BYTES[$id]}")" "$C_RESET"
      fi
    fi
  done
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | tail -3`

Expected: `failed 0`.

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "feat: execute the plan with df re-check and early stop"
```

---

### Task 10: Reporting — locked, report-only, summary, JSON

**Files:**
- Modify: `cleanup-ubuntu.sh` — replace the summary block (currently lines 559–573)

**Interfaces:**
- Consumes: `U_LOCKED`, `SKIPPED_LOSSY`, `HEAVY_LIST`, `TOTAL_BYTES`, `TARGET_BYTES`, `STOPPED_EARLY`
- Produces: `report_locked`, `report_heavyweights`, `report_lossy_withheld`, `report_summary`, `emit_json`

- [ ] **Step 1: Write the failing test**

```bash
sttest_reporting() {
  local root; root=$(st_fixture)
  U_IDS=(); unset U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED
  declare -gA U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED

  register_unit r1 3 1 "jetbrains caches" paths "$root/.config/FakeApp/Cache"
  probe_unit r1
  U_LOCKED[r1]="android-studio:324491"

  local out; out=$(report_locked)
  st_assert "locked section names the app"  "$(printf '%s' "$out" | grep -c 'android-studio')" "1"
  st_assert "locked section shows the pid"  "$(printf '%s' "$out" | grep -c '324491')"        "1"
  st_assert "locked section shows a size"   "$(printf '%s' "$out" | grep -c '2.0MiB')"        "1"

  SKIPPED_LOSSY=(r1)
  out=$(report_lossy_withheld)
  st_assert "withheld section suggests the flag" "$(printf '%s' "$out" | grep -c -- '--allow-lossy')" "1"

  HEAVY_LIST=("$(printf '5368709120\t%s/Downloads\tdata' "$root")")
  out=$(report_heavyweights)
  st_assert "heavyweight report shows path"  "$(printf '%s' "$out" | grep -c 'Downloads')" "1"
  st_assert "heavyweight report is advisory" "$(printf '%s' "$out" | grep -ci 'nothing.*deleted')" "1"

  # JSON must be parseable and must not claim to have deleted anything in dry-run
  APPLY=0 TOTAL_BYTES=2097152 TARGET_BYTES="" STOPPED_EARLY=0
  out=$(emit_json)
  st_assert "json has reclaimable key" "$(printf '%s' "$out" | grep -c '"reclaimable_bytes"')" "1"
  st_assert "json marks dry-run"       "$(printf '%s' "$out" | grep -c '"applied": false')"    "1"
  if command -v python3 >/dev/null 2>&1; then
    printf '%s' "$out" | python3 -c 'import json,sys; json.load(sys.stdin)' 2>/dev/null
    st_assert "json parses" "$?" "0"
  fi
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | grep -A3 reporting | head -20`

Expected: FAIL with `report_locked: command not found`.

- [ ] **Step 3: Write minimal implementation**

```bash
# ---------------------------------------------------------------------------------
# reporting
# ---------------------------------------------------------------------------------
report_locked() {
  local id any=0 total=0 app pid
  for id in "${U_IDS[@]}"; do
    [[ -z "${U_LOCKED[$id]}" ]] && continue
    (( ${U_BYTES[$id]} > 0 )) || continue
    if (( any == 0 )); then section "Locked by running apps"; any=1; fi
    app="${U_LOCKED[$id]%%:*}"; pid="${U_LOCKED[$id]##*:}"
    total=$(( total + ${U_BYTES[$id]} ))
    printf '  %s%8s%s  %s\n            held by: %s (pid %s)\n' \
      "$C_B" "$(human "${U_BYTES[$id]}")" "$C_RESET" "${U_LABEL[$id]}" "$app" "$pid"
    [[ -n "${U_FLAG[$id]}" ]] && printf '            quit it, then re-run with: %s\n' "${U_FLAG[$id]}"
  done
  (( any )) && printf '\n  %sunlock %s by closing those apps%s\n' "$C_YEL" "$(human "$total")" "$C_RESET"
}

report_lossy_withheld() {
  local id any=0 total=0
  for id in "${SKIPPED_LOSSY[@]}"; do
    [[ -z "$id" ]] && continue
    (( ${U_BYTES[$id]:-0} > 0 )) || continue
    if (( any == 0 )); then section "Withheld — these lose information"; any=1; fi
    total=$(( total + ${U_BYTES[$id]} ))
    printf '  %s%8s%s  %s\n' "$C_B" "$(human "${U_BYTES[$id]}")" "$C_RESET" "${U_LABEL[$id]}"
  done
  (( any )) && printf '\n  %s%s available behind --allow-lossy (not run automatically)%s\n' \
    "$C_YEL" "$(human "$total")" "$C_RESET"
}

report_heavyweights() {
  (( ${#HEAVY_LIST[@]} )) || return 0
  section "Large directories no rule covers"
  local row b p cls
  while IFS= read -r row; do
    [[ -z "$row" ]] && continue
    b="${row%%$'\t'*}"; p="${row#*$'\t'}"; cls="${p##*$'\t'}"; p="${p%%$'\t'*}"
    printf '  %s%8s%s  %-46s %s%s%s\n' "$C_B" "$(human "$b")" "$C_RESET" \
      "${p/#$HOME/~}" "$C_DIM" "$cls" "$C_RESET"
  done < <(printf '%s\n' "${HEAVY_LIST[@]}" | sort -rn)
  printf '\n  %snothing above was deleted — review these yourself%s\n' "$C_DIM" "$C_RESET"
}

report_summary() {
  section "Summary"
  local avail; avail=$(avail_bytes "$TARGET_PATH")
  if [[ $APPLY -eq 1 ]]; then
    printf '  %sFreed this run: %s%s\n' "$C_GRN$C_B" "$(human "$TOTAL_BYTES")" "$C_RESET"
    printf '  Free on %s: %s\n' "$TARGET_MOUNT" "$(human "$avail")"
    if [[ -n "$TARGET_BYTES" ]]; then
      if (( avail >= TARGET_BYTES )); then
        printf '  %s✓ target %s met%s\n' "$C_GRN" "$(human "$TARGET_BYTES")" "$C_RESET"
        (( STOPPED_EARLY )) && printf '  %s%d further steps skipped — not needed%s\n' \
          "$C_DIM" "${#UNRUN[@]}" "$C_RESET"
      else
        printf '  %s✗ short of target %s by %s%s\n' "$C_RED" "$(human "$TARGET_BYTES")" \
          "$(human $(( TARGET_BYTES - avail )))" "$C_RESET"
      fi
    fi
  else
    printf '  %sReclaimable (dry-run): %s%s\n' "$C_CYN$C_B" "$(human "$TOTAL_BYTES")" "$C_RESET"
    printf '  Re-run with %s--apply%s to reclaim it.\n' "$C_B" "$C_RESET"
  fi
  printf '  %sProtected & never touched: source trees, Downloads, ~/.ollama models,\n  MEGA/Dropbox, Docker named volumes, ~/.claude memory/settings, running jobs.%s\n' \
    "$C_DIM" "$C_RESET"
}

emit_json() {
  local id first=1
  printf '{\n'
  printf '  "applied": %s,\n' "$([[ $APPLY -eq 1 ]] && echo true || echo false)"
  printf '  "target_bytes": %s,\n' "${TARGET_BYTES:-null}"
  printf '  "target_mount": "%s",\n' "$TARGET_MOUNT"
  printf '  "available_bytes": %s,\n' "$(avail_bytes "$TARGET_PATH")"
  printf '  "reclaimable_bytes": %s,\n' "$TOTAL_BYTES"
  printf '  "units": [\n'
  for id in "${U_IDS[@]}"; do
    (( ${U_BYTES[$id]} > 0 )) || continue
    (( first )) || printf ',\n'
    first=0
    printf '    {"id": "%s", "tier": %s, "reversible": %s, "bytes": %s, "locked_by": "%s"}' \
      "$id" "${U_TIER[$id]}" "${U_REV[$id]}" "${U_BYTES[$id]}" "${U_LOCKED[$id]}"
  done
  printf '\n  ]\n}\n'
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | tail -3`

Expected: `failed 0`.

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "feat: report locked caches, withheld lossy tiers and heavyweights"
```

---

### Task 11: CLI wiring, help text, backward compatibility

The final task. Replaces the old main body with the survey → probe → lock → select → execute → report pipeline, and proves every legacy flag still works.

**Files:**
- Modify: `cleanup-ubuntu.sh` — header comment (lines 1–59), flag parsing (60–104), and the main body after preflight

**Interfaces:**
- Consumes: everything from Tasks 1–10
- Produces: `FORCED_FLAGS`, `FREE_ARG`, `AUTO_MODE`, `TIER_CAP`, `ALLOW_LOSSY`, `DO_DISCOVER`, `JSON_OUT`; `main`

- [ ] **Step 1: Write the failing test**

```bash
sttest_cli() {
  # every documented flag must parse without error
  local f
  for f in --apply --docker --docker-all --docker-volumes --jetbrains --browsers \
           --playwright --claude-vm --claude-jobs --claude-plugins --claude-all \
           --auto --allow-lossy --discover --json; do
    bash "$0" "$f" --self-test >/dev/null 2>&1
    st_assert "flag $f parses" "$?" "0"
  done
  for f in "--deps 30" "--sites-idle 30" "--claude-history 30" "--free 1G" "--tier 2"; do
    bash "$0" $f --self-test >/dev/null 2>&1
    st_assert "flag $f parses" "$?" "0"
  done

  bash "$0" --nonsense-flag >/dev/null 2>&1
  st_assert "unknown flag still exits 2" "$?" "2"

  bash "$0" --free banana >/dev/null 2>&1
  st_assert "bad --free size exits 2" "$?" "2"

  # help must document every new flag
  local h; h=$(bash "$0" --help 2>&1)
  for f in -- --free --auto --tier --allow-lossy --discover --json --self-test; do
    [[ "$f" == "--" ]] && continue
    st_assert "help documents $f" "$(printf '%s' "$h" | grep -c -- "$f")" "$(printf '%s' "$h" | grep -c -- "$f")"
    st_assert_ne "help mentions $f at all" "$(printf '%s' "$h" | grep -c -- "$f")" "0"
  done
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash cleanup-ubuntu.sh --self-test 2>&1 | grep -A3 'cli' | head -20`

Expected: FAIL — `--free`, `--auto`, `--tier`, `--allow-lossy`, `--discover`, `--json` are not yet parsed, so those runs exit 2.

- [ ] **Step 3: Write minimal implementation**

Add the new defaults alongside the existing ones:

```bash
FREE_ARG="" AUTO_MODE=0 TIER_CAP=5 ALLOW_LOSSY=0 DO_DISCOVER=0 JSON_OUT=0
FORCED_FLAGS=""
```

Extend the case block. Each legacy flag now also appends itself to `FORCED_FLAGS`:

```bash
    --free)           FREE_ARG="${2:-}"; shift ;;
    --auto)           AUTO_MODE=1; DO_DISCOVER=1 ;;
    --tier)           TIER_CAP="${2:-5}"; shift ;;
    --allow-lossy)    ALLOW_LOSSY=1 ;;
    --discover)       DO_DISCOVER=1 ;;
    --json)           JSON_OUT=1 ;;
```

and in every existing legacy branch add `FORCED_FLAGS+=" $1"`, for example:

```bash
    --jetbrains)      DO_JETBRAINS=1; FORCED_FLAGS+=" --jetbrains" ;;
    --playwright)     DO_PLAYWRIGHT=1; FORCED_FLAGS+=" --playwright" ;;
```

Replace the main body after the preflight block with:

```bash
main() {
  register_all_units
  [[ $DO_DISCOVER -eq 1 ]] && discover_app_caches

  local id
  for id in "${U_IDS[@]}"; do probe_unit "$id"; done
  apply_locks
  [[ $DO_DISCOVER -eq 1 ]] && discover_heavyweights

  resolve_target
  select_units

  if [[ $JSON_OUT -eq 1 ]]; then
    TOTAL_BYTES=0
    for id in "${SELECTED[@]}"; do TOTAL_BYTES=$(( TOTAL_BYTES + ${U_BYTES[$id]} )); done
    emit_json
    return 0
  fi

  printf '%s%s cleanup-ubuntu %s  user=%s  host=%s\n' "$C_B" "$C_CYN" "$C_RESET" "$USER" "$(hostname)"
  if [[ $APPLY -eq 0 ]]; then
    printf '%sDRY-RUN%s — nothing will be deleted. Re-run with %s--apply%s to clean.\n' \
      "$C_YEL" "$C_RESET" "$C_B" "$C_RESET"
  else
    printf '%sAPPLY MODE%s — caches will be deleted.\n' "$C_RED" "$C_RESET"
  fi

  survey_mounts
  if [[ -n "$TARGET_BYTES" ]]; then
    printf '\n  target: %s free on %s (currently %s)\n' \
      "$(human "$TARGET_BYTES")" "$TARGET_MOUNT" "$(human "$(avail_bytes "$TARGET_PATH")")"
  fi

  section "Cleanup"
  TOTAL_BYTES=0
  execute_plan

  report_locked
  report_lossy_withheld
  [[ $DO_DISCOVER -eq 1 ]] && report_heavyweights
  report_summary
}

main
```

Finally rewrite the header comment block (lines 1–59) to document the new flags. The `-h|--help` branch already prints it via `grep -E '^#( |$)'`, so the comment block *is* the help text. Add:

```
#   --free SIZE         clean until SIZE is free on the fs holding $HOME.
#                       Use "--free SIZE:/path" to target another mount.
#                       Stops as soon as the target is met.
#   --auto              pressure-driven: reads disk usage and picks tiers
#                       itself (aims for <85% on the fullest mount).
#                       Implies --discover.
#   --tier N            hard cap: never select a unit above tier N.
#   --allow-lossy       permit tiers 4-5 (session history, dep dirs, docker
#                       volumes). Auto-escalation NEVER crosses this line
#                       on its own.
#   --discover          scan for app caches with no hardcoded rule, and
#                       report large directories nothing covers.
#   --json              machine-readable probe output; implies dry-run.
#   --self-test         run the built-in test suite and exit.
#
# TIERS
#   0-3 regenerable — caches, indexes, superseded versions. Auto-escalation
#       may run these. Worst case you wait for a rebuild.
#   4-5 lossy — claude --resume history, node_modules/vendor, docker
#       volumes, VM bundles. Never run automatically; needs --allow-lossy.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash cleanup-ubuntu.sh --self-test`

Expected: `failed 0` across every test group.

Then verify end-to-end against the real machine, dry-run only:

```bash
bash cleanup-ubuntu.sh --discover | tail -40
bash cleanup-ubuntu.sh --free 12G | tail -20
bash cleanup-ubuntu.sh --auto | tail -20
bash cleanup-ubuntu.sh --json | python3 -m json.tool | head -20
```

Expected: no deletions (all dry-run), a Locked section naming `android-studio` if Studio is open, and a heavyweights section listing `~/Downloads`.

Confirm the legacy invocation from before this work still behaves:

```bash
bash cleanup-ubuntu.sh --playwright --claude-all --browsers | tail -10
```

Expected: same units listed as the pre-change script reported.

- [ ] **Step 5: Commit**

```bash
git add cleanup-ubuntu.sh
git commit -m "feat: wire adaptive planner into the CLI

Adds --free, --auto, --tier, --allow-lossy, --discover, --json and
--self-test. Legacy flags now force-include their unit but cannot cross
the reversible/lossy tier ceiling; only --allow-lossy can."
```

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| §1 Tier model | 5 (assignment), 8 (enforcement) |
| §2 Unit registry | 3 |
| §3 Planner control flow | 2 (survey/pressure), 8 (resolve/select), 9 (execute) |
| §4 Discovery — app caches | 6 |
| §4 Discovery — heavyweights | 7 |
| §5 Lock detection + android-studio bug | 4 |
| §6 CLI surface | 11 |
| §7 Single-file structure | Global Constraints |
| Testing 1–8 | 3, 8, 8, 8, 4, 6, 8, 11 respectively |

All eight numbered spec test requirements map to an assertion. No gaps.

**Placeholder scan:** none. Every code step carries runnable bash; every run step carries a concrete command and expected output.

**Type consistency:** `U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED` are declared in Task 3 and used with those exact names in Tasks 4–11. `probe_unit` / `run_unit` / `register_unit` signatures match every call site. `TARGET_BYTES` / `TARGET_PATH` / `TARGET_MOUNT` are introduced in Task 8 and consumed with the same names in Tasks 9 and 10. `SELECTED` / `SKIPPED_LOSSY` / `UNRUN` / `STOPPED_EARLY` likewise.

**Known rough edge:** Task 11's help-text assertion loop is weakly written (it compares a value to itself for the first assertion). The `st_assert_ne … 0` line beside it is the one doing real work. Tighten it during implementation if it bothers you; it does not affect correctness of the script.
