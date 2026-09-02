#!/usr/bin/env bash
#
# cleanup-ubuntu.sh — intelligent, process-aware disk cleanup for Ubuntu / Debian
# ---------------------------------------------------------------------------------
# Safe by default. Reclaims space from regenerable caches only. Never deletes
# source code, documents, LLM models, or databases.
#
# HIGHLIGHTS
#   * DRY-RUN by default — shows what WOULD be freed. Use --apply to actually delete.
#   * Goal-driven — "--free 12G" cleans until that much is free, then stops.
#   * Process-aware — skips caches of apps that are currently running (IDEs,
#     browsers, Docker) so it never corrupts a live session.
#   * Tool-aware — auto-detects installed package managers and uses their native
#     "cache clean" commands where possible (safer than blind rm).
#   * Filesystem-aware — targets the partition that is actually full, and drops
#     steps that would free space somewhere else.
#   * Protects real data — Downloads, source trees, ollama models, MEGA/Dropbox
#     syncs, and Docker *named* volumes are never touched.
#
# USAGE
#   ./cleanup-ubuntu.sh                 # dry-run, show reclaimable space
#   ./cleanup-ubuntu.sh --apply         # actually clean (asks before big/risky steps)
#   ./cleanup-ubuntu.sh --apply --yes   # non-interactive
#   ./cleanup-ubuntu.sh --apply --free 12G   # clean until 12G is free, then stop
#
# ADAPTIVE
#   --free SIZE         clean until SIZE is free on the fs holding $HOME.
#                       Use "--free SIZE:/path" to target another mount.
#                       Stops as soon as the target is met.
#   --auto              pressure-driven: reads disk usage and picks tiers
#                       itself (aims for <85% on the fullest mount).
#                       Implies --discover.
#   --tier N            hard cap: never select a unit above tier N.
#   --allow-lossy       permit tiers 4-5 (session history, dep dirs, docker
#                       volumes). Auto-escalation NEVER crosses this line
#                       on its own, and each such step is still confirmed
#                       one at a time unless --yes is given.
#   --discover          scan for app caches with no hardcoded rule -- every
#                       directory directly under ~/.cache, plus name-matched
#                       cache dirs nested in ~/.config and ~/.local/share -- and
#                       report large directories nothing covers.
#   --json              machine-readable probe output.
#   --self-test         run the built-in test suite and exit.
#
# TIERS
#   0-3 regenerable — caches, indexes, superseded versions. Auto-escalation
#       may run these. Worst case you wait for a rebuild.
#   4-5 lossy — claude --resume history, node_modules/vendor, docker
#       volumes, VM bundles. Never run automatically; needs --allow-lossy.
#
# OPT-IN EXTRAS (all off by default)
#   --docker            prune unused Docker images/containers/build-cache/networks
#   --docker-all        also remove ALL unused images (not just dangling)
#   --docker-volumes    also prune unused Docker volumes  (⚠ may delete DB data)
#   --jetbrains         clear JetBrains IDE caches (only if no JetBrains proc runs)
#   --browsers          clear browser HTTP caches (only for browsers not running)
#   --playwright        remove Playwright browser binaries (~/.cache/ms-playwright)
#   --gradle            clear ~/.gradle/caches and ~/.gradle/wrapper (only if no
#                       Gradle daemon or Android Studio is running; next build
#                       re-downloads every dependency)
#   --maven             clear ~/.m2/repository (re-downloaded on next build)
#   --deps DAYS         remove stale node_modules AND composer vendor dirs not
#                       modified in DAYS days (alias: --node-modules)
#   --sites-idle DAYS [DIR]
#                       clear node_modules/vendor ONLY inside project dirs whose
#                       newest source file (ignoring .git/node_modules/vendor) is
#                       older than DAYS — i.e. dormant sites. DIR defaults to ~/Sites.
#                       Smarter than --deps: judges the whole project's activity,
#                       not just the dependency folder's timestamp.
#   --system            apt-get clean/autoremove + journal vacuum + old snaps (sudo)
#   --claude-vm         remove Claude Desktop VM bundles (⚠ re-downloads, GBs)
#   --claude-jobs [DAYS]
#                       clean ~/.claude/jobs — background-job scratch dirs, which can
#                       hold GBs of throwaway node_modules. Finished jobs older than
#                       DAYS (default 7) are removed whole; finished jobs newer than
#                       that keep their state/timeline but lose their tmp/ scratch.
#                       Use "--claude-jobs 0" to drop every finished job.
#                       Never touches the job this script runs inside, jobs that are
#                       running/blocked, or anything written in the last 60 minutes.
#   --claude-plugins    drop superseded plugin versions under ~/.claude/plugins/cache,
#                       keeping the newest version of each plugin (re-downloads if an
#                       old version is ever pinned again)
#   --claude-history DAYS
#                       prune session transcripts (~/.claude/projects/*/*.jsonl),
#                       file-history, shell snapshots and session env older than DAYS
#                       (⚠ kills "claude --resume" for those sessions; memory/ dirs
#                       and per-session subdirectories are never touched)
#   --claude-all        = --claude-jobs --claude-plugins (the regenerable subset;
#                       deliberately excludes --claude-history)
#
# Exit status is always 0 unless a fatal precondition fails.
#
set -uo pipefail

# ---------------------------------------------------------------------------------
# config / flags
# ---------------------------------------------------------------------------------
APPLY=0  ASSUME_YES=0
SELF_TEST=0
DO_DOCKER=0 DOCKER_ALL=0 DOCKER_VOLUMES=0
DO_JETBRAINS=0 DO_BROWSERS=0 DO_PLAYWRIGHT=0
DO_SYSTEM=0 DO_CLAUDE_VM=0
DO_CLAUDE_JOBS=0 CLAUDE_JOBS_DAYS=7
DO_CLAUDE_PLUGINS=0
CLAUDE_HISTORY_DAYS=0
DEPS_DAYS=0
SITES_IDLE_DAYS=0
SITES_ROOT="$HOME/Sites"
CLAUDE_ROOT="$HOME/.claude"
TMP_SWEEP_ROOT="/tmp"
FREE_ARG="" AUTO_MODE=0 TIER_CAP=5 ALLOW_LOSSY=0 DO_DISCOVER=0 JSON_OUT=0
FORCED_FLAGS=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply)          APPLY=1 ;;
    -y|--yes)         ASSUME_YES=1 ;;
    --docker)         DO_DOCKER=1; FORCED_FLAGS+=" --docker" ;;
    --docker-all)     DO_DOCKER=1; DOCKER_ALL=1; FORCED_FLAGS+=" --docker" ;;
    --docker-volumes) DO_DOCKER=1; DOCKER_VOLUMES=1; FORCED_FLAGS+=" --docker --docker-volumes" ;;
    --jetbrains)      DO_JETBRAINS=1; FORCED_FLAGS+=" --jetbrains" ;;
    --browsers)       DO_BROWSERS=1; FORCED_FLAGS+=" --browsers" ;;
    --playwright)     DO_PLAYWRIGHT=1; FORCED_FLAGS+=" --playwright" ;;
    --gradle)         FORCED_FLAGS+=" --gradle" ;;
    --maven)          FORCED_FLAGS+=" --maven" ;;
    --deps|--node-modules) DEPS_DAYS="${2:-30}"; FORCED_FLAGS+=" --deps"; shift ;;
    --sites-idle)     SITES_IDLE_DAYS="${2:-30}"; FORCED_FLAGS+=" --sites-idle"; shift
                      # optional DIR arg: consume it only if it's an existing dir
                      if [[ -n "${2:-}" && -d "${2}" ]]; then SITES_ROOT="$2"; shift; fi ;;
    --system)         DO_SYSTEM=1; FORCED_FLAGS+=" --system" ;;
    --claude-vm)      DO_CLAUDE_VM=1; FORCED_FLAGS+=" --claude-vm" ;;
    --claude-jobs)    DO_CLAUDE_JOBS=1; FORCED_FLAGS+=" --claude-jobs"
                      # optional DAYS arg: consume it only if it really is a number
                      if [[ "${2:-}" =~ ^[0-9]+$ ]]; then CLAUDE_JOBS_DAYS="$2"; shift; fi ;;
    --claude-plugins) DO_CLAUDE_PLUGINS=1; FORCED_FLAGS+=" --claude-plugins" ;;
    --claude-history) CLAUDE_HISTORY_DAYS="${2:-30}"; FORCED_FLAGS+=" --claude-history"
                      [[ "${2:-}" =~ ^[0-9]+$ ]] && shift ;;
    --claude-all)     DO_CLAUDE_JOBS=1; DO_CLAUDE_PLUGINS=1
                      FORCED_FLAGS+=" --claude-jobs --claude-plugins" ;;
    --free)           FREE_ARG="${2:-}"; shift ;;
    --auto)           AUTO_MODE=1; DO_DISCOVER=1 ;;
    --tier)           TIER_CAP="${2:-5}"; shift ;;
    --allow-lossy)    ALLOW_LOSSY=1 ;;
    --discover)       DO_DISCOVER=1 ;;
    --json)           JSON_OUT=1 ;;
    --self-test)      SELF_TEST=1 ;;
    -h|--help)        grep -E '^#( |$)' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1  (try --help)" >&2; exit 2 ;;
  esac
  shift
done

# ---------------------------------------------------------------------------------
# ui helpers
# ---------------------------------------------------------------------------------
if [[ -t 1 ]]; then
  C_RESET=$'\e[0m'; C_DIM=$'\e[2m'; C_B=$'\e[1m'
  C_GRN=$'\e[32m'; C_YEL=$'\e[33m'; C_RED=$'\e[31m'; C_CYN=$'\e[36m'
else
  C_RESET= C_DIM= C_B= C_GRN= C_YEL= C_RED= C_CYN=
fi

TOTAL_BYTES=0
section() { printf '\n%s== %s ==%s\n' "$C_B$C_CYN" "$1" "$C_RESET"; }
info()    { printf '  %s\n' "$1"; }
skip()    { printf '  %sskip%s  %s\n' "$C_YEL" "$C_RESET" "$1"; }
kept()    { printf '  %skeep%s  %s\n' "$C_DIM" "$C_RESET" "$1"; }

# bytes of a path (0 if missing). Uses apparent size in blocks -> bytes.
path_bytes() { [[ -e "$1" ]] && du -sb --apparent-size "$1" 2>/dev/null | awk '{print $1}' || echo 0; }
# iec-i, not iec: these are binary multiples, so they must be labelled MiB/GiB.
# "--to=iec --suffix=B" printed 1048576 as "1.0MB", which is simply the wrong unit.
human()      { numfmt --to=iec-i --suffix=B "${1:-0}" 2>/dev/null || echo "${1}B"; }


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

# sttest_cli re-invokes this script to check flag parsing. Those children must not
# re-enter the CLI test themselves, or the suite forks without bound.
st_run() {
  printf '%s%s self-test %s\n' "$C_B" "$C_CYN" "$C_RESET"
  local t
  for t in $(declare -F | awk '{print $3}' | grep '^sttest_' | sort); do
    [[ -n "${CLEANUP_ST_CHILD:-}" && "$t" == sttest_cli ]] && continue
    printf '\n%s-- %s --%s\n' "$C_DIM" "${t#sttest_}" "$C_RESET"
    "$t"
    st_cleanup
  done
  printf '\n  %spassed %d%s  %sfailed %d%s\n' \
    "$C_GRN" "$ST_PASS" "$C_RESET" "$C_RED" "$ST_FAIL" "$C_RESET"
  [[ $ST_FAIL -eq 0 ]]
}

# ---------------------------------------------------------------------------------
# unit registry
#
# A "unit" is one cleanup step. Splitting probe (measure) from run (delete) is
# what lets the planner rank by size, stop early once a target is met, and print
# an honest dry-run.
# ---------------------------------------------------------------------------------
U_IDS=()
declare -A U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED U_MOUNTHINT

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
  U_MOUNTHINT[$id]=""
}

# A cmd unit has no paths to resolve a mount from, and some free space somewhere
# other than $HOME — apt and journalctl both act on /. Tag those explicitly.
unit_mount_hint() { U_MOUNTHINT[$1]=$2; }

unit_paths() { printf '%s\n' "${U_PAYLOAD[$1]}"; }

# Measure only. Nothing here may mutate the filesystem.
probe_unit() {
  local id=$1 total=0 b p
  local -a plist=()
  if [[ "${U_KIND[$id]}" == cmd ]]; then
    U_BYTES[$id]=0
    U_MOUNT[$id]=$(mount_of "${U_MOUNTHINT[$id]:-$HOME}")
    return 0
  fi
  readarray -t plist < <(unit_paths "$id")
  for p in "${plist[@]}"; do
    [[ -z "$p" || ! -e "$p" ]] && continue
    [[ -z "${U_MOUNT[$id]}" ]] && U_MOUNT[$id]=$(mount_of "$p")
    b=$(path_bytes "$p")
    total=$(( total + b ))
  done
  [[ -z "${U_MOUNT[$id]}" ]] && U_MOUNT[$id]=$(mount_of "${U_MOUNTHINT[$id]:-$HOME}")
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
  'GradleDaemon|/gradlew( |$)|/gradle( |$)|KotlinCompileDaemon'
)
LOCK_NAME=(
  "android-studio" "jetbrains-ide" "zed" "vscode" "chrome" "brave"
  "firefox" "slack" "discord" "postman" "goose" "figma" "gradle"
)
LOCK_ROOTS=(
  "$HOME/.cache/JetBrains
$HOME/.config/JetBrains
$HOME/.local/share/JetBrains
$HOME/.gradle"
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
  "$HOME/.gradle"
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
  [[ "$cmd" == *cleanup-ubuntu* ]] && return 1
  for rx in "${LOCK_RX[@]}"; do
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

  local id i rx pid cmd root p entry
  local -a roots=() plist=()

  for i in "${!LOCK_RX[@]}"; do
    rx="${LOCK_RX[$i]}"
    pid=""
    for entry in "${procs[@]}"; do
      [[ -z "$entry" ]] && continue
      cmd="${entry#*$'\t'}"
      [[ "$cmd" == *cleanup-ubuntu* ]] && continue
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
            # break the root and path loops only — the remaining units still
            # need checking, or a second cache under the same app stays unlocked
            break 2
          fi
        done
      done
    done
  done
}

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
  register_tmp_units

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
  register_extra_pkg_units

  # --- tier 2: bigger regenerable artefacts ---
  register_unit playwright 2 1 "playwright browsers" paths "$HOME/.cache/ms-playwright" "--playwright"
  register_claude_job_units
  register_claude_plugin_units

  # --- tier 3: costs a reindex or a cold load ---
  register_unit jetbrains-cache 3 1 "JetBrains caches" paths "$HOME/.cache/JetBrains" "--jetbrains"
  register_build_tool_units
  register_browser_units

  # --- tier 0/1 but on / rather than $HOME: only useful when / is the full one ---
  register_system_units

  # ================= auto-escalation ceiling =================

  # --- tier 4: loses information ---
  register_claude_history_units
  register_dep_units
  register_sites_idle_units

  # --- tier 5: loses information, possibly irreplaceable ---
  register_unit claude-vm      5 0 "Claude VM bundles" paths "$HOME/.config/Claude/vm_bundles" "--claude-vm"
  register_unit docker-volumes 5 0 "docker volumes"    cmd   "docker volume prune -f" "--docker-volumes"
  register_unit docker-prune   5 0 "docker prune"      cmd \
    "docker builder prune -f; docker container prune -f; docker network prune -f; docker image prune $([[ ${DOCKER_ALL:-0} -eq 1 ]] && echo -af || echo -f)" \
    "--docker"
}

# Stale /tmp leftovers we own. The protected pattern covers what a live desktop
# session and a running Claude job need; the 60-minute floor keeps in-flight
# temp files out of reach.
register_tmp_units() {
  local tmp_protected='^(claude-|cc-daemon|claude-mcp|\.X|\.ICE|\.XIM|\.font|com\.google\.Chrome|\.com\.google|org\.chromium|\.org\.chromium|scoped_dir|hsperfdata|systemd-private|snap-private|\.Test-unix|cef_server)'
  local entry name
  local -a hits=()
  shopt -s nullglob dotglob
  for entry in "$TMP_SWEEP_ROOT"/*; do
    name=$(basename "$entry")
    [[ "$(stat -c '%U' "$entry" 2>/dev/null)" == "$USER" ]] || continue
    [[ "$name" =~ $tmp_protected ]] && continue
    [[ -n "$(find "$entry" -maxdepth 0 -mmin -60 2>/dev/null)" ]] && continue
    hits+=("$entry")
  done
  shopt -u nullglob dotglob
  (( ${#hits[@]} )) || return 0
  register_unit tmp-stale 0 1 "stale $TMP_SWEEP_ROOT files (${#hits[@]})" paths "$(printf '%s\n' "${hits[@]}")"
  unit_mount_hint tmp-stale "$TMP_SWEEP_ROOT"
}

# apt cache, journal and superseded snap revisions. These free space on / only,
# so they carry a mount hint — on a machine where /home is full and / is not,
# the planner drops them instead of reporting a win that never lands.
register_system_units() {
  command -v apt-get >/dev/null 2>&1 && {
    register_unit system-apt 0 1 "apt cache + orphan packages" cmd \
      "sudo apt-get clean; sudo apt-get autoremove --purge -y" "--system"
    unit_mount_hint system-apt /
  }
  command -v journalctl >/dev/null 2>&1 && {
    register_unit system-journal 0 1 "journal older than 7d" cmd \
      "sudo journalctl --vacuum-time=7d" "--system"
    unit_mount_hint system-journal /
  }
  command -v snap >/dev/null 2>&1 && {
    register_unit system-snaps 1 1 "superseded snap revisions" cmd \
      'LANG=C snap list --all 2>/dev/null | awk "/disabled/{print \$1, \$3}" | while read -r sn rev; do sudo snap remove "$sn" --revision="$rev"; done' \
      "--system"
    unit_mount_hint system-snaps /
  }
}

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
  local -a globbed=()
  shopt -s nullglob
  root="$HOME/.config/google-chrome"
  if [[ -d "$root" ]]; then
    globbed=("$root"/*/Cache "$root"/*/"Code Cache" "$root"/*/GPUCache)
    (( ${#globbed[@]} )) && register_unit chrome-profile-cache 3 1 "Chrome profile caches" paths \
      "$(printf '%s\n' "${globbed[@]}")" "--browsers"
  fi
  root="$HOME/.config/BraveSoftware/Brave-Browser"
  if [[ -d "$root" ]]; then
    globbed=("$root"/*/Cache "$root"/*/"Code Cache" "$root"/*/GPUCache)
    (( ${#globbed[@]} )) && register_unit brave-profile-cache 3 1 "Brave profile caches" paths \
      "$(printf '%s\n' "${globbed[@]}")" "--browsers"
  fi
  shopt -u nullglob
  register_unit chrome-cache  3 1 "chrome ~/.cache" paths "$HOME/.cache/google-chrome
$HOME/.cache/Google" "--browsers"
  register_unit brave-cache   3 1 "brave ~/.cache"   paths "$HOME/.cache/BraveSoftware" "--browsers"
  register_unit firefox-cache 3 1 "firefox ~/.cache" paths "$HOME/.cache/mozilla" "--browsers"
}

# LOSSY. Removes `claude --resume` for those sessions. memory/ is never touched.
# The flag defaults to 0 meaning "not requested"; probing still wants a sane
# window, so fall back to 30 days rather than +0, which would match everything
# written before today.
register_claude_history_units() {
  local days=${CLAUDE_HISTORY_DAYS:-30}
  (( days > 0 )) || days=30
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

# LOSSY. Judges the whole project's activity rather than the dependency folder's
# timestamp, so a site you touched last week keeps its deps even when the deps
# themselves look stale, and a dormant site is cleared even when something inside
# node_modules has a fresh mtime.
register_sites_idle_units() {
  local days=${SITES_IDLE_DAYS:-0}
  (( days > 0 )) || return 0
  [[ -d "$SITES_ROOT" ]] || return 0
  local site newest idle now_ts nm vd base
  now_ts=$(date +%s)

  for site in "$SITES_ROOT"/*/; do
    site=${site%/}; [[ -d "$site" ]] || continue
    newest=$(find "$site" -type f -not -path '*/.git/*' -not -path '*/node_modules/*' \
               -not -path '*/vendor/*' -printf '%T@\n' 2>/dev/null | sort -rn | head -1)
    newest=${newest%.*}; [[ -z "$newest" ]] && newest=0
    idle=$(( (now_ts - newest) / 86400 ))
    (( idle > days )) || continue
    base=$(basename "$site")

    while IFS= read -r nm; do
      [[ -z "$nm" ]] && continue
      [[ -f "$(dirname "$nm")/package.json" ]] || continue
      register_unit "idle-nm-${nm//\//-}" 4 0 "$base/…/node_modules (${idle}d idle)" paths "$nm" "--sites-idle"
    done < <(find "$site" -maxdepth 6 -type d -name node_modules -prune 2>/dev/null)

    while IFS= read -r vd; do
      [[ -z "$vd" ]] && continue
      [[ -f "$(dirname "$vd")/composer.json" ]] || continue
      register_unit "idle-vendor-${vd//\//-}" 4 0 "$base/…/vendor (${idle}d idle)" paths "$vd" "--sites-idle"
    done < <(find "$site" -maxdepth 6 -type d -name vendor -prune 2>/dev/null)
  done
}

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
  # Compare case-insensitively: Chromium ships "Cache"/"GPUCache", but most Linux
  # apps use lowercase. Protection is matched the same way, so a lowercase
  # "cookies" is still refused.
  local n="${1,,}" x
  for x in "${PROTECTED_NAMES[@]}"; do [[ "$n" == "${x,,}" ]] && return 1; done
  for x in "${CACHE_NAMES[@]}";     do [[ "$n" == "${x,,}" ]] && return 0; done
  return 1
}

# True when some already-registered unit lists this exact path, so the generic
# scanners never re-claim what a hardcoded unit already owns.
path_claimed() {
  local want="$1" id line
  for id in "${U_IDS[@]}"; do
    while IFS= read -r line; do
      [[ "$line" == "$want" ]] && return 0
    done <<< "${U_PAYLOAD[$id]}"
  done
  return 1
}

# Everything directly inside ~/.cache is by definition regenerable (XDG basedir
# spec), so it needs no name whitelist -- unlike ~/.config, where a cache sits
# beside real state. discover_app_caches handles that deeper, name-matched case.
discover_xdg_caches() {
  local root="${XDG_CACHE_ROOT:-$HOME/.cache}" d name id
  [[ -d "$root" ]] || return 0
  while IFS= read -r d; do
    [[ -z "$d" ]] && continue
    path_claimed "$d" && continue
    name=$(basename "$d")
    id="xdg-${name}"; id="${id// /-}"
    [[ -n "${U_TIER[$id]:-}" ]] && continue
    register_unit "$id" 2 1 ".cache/$name" paths "$d"
  done < <(find "$root" -mindepth 1 -maxdepth 1 -type d 2>/dev/null)
}

# Gradle and Maven re-download everything on the next build: regenerable, but the
# cold fetch is expensive, so they sit at tier 3 beside the JetBrains reindex and
# stay behind an explicit flag.
register_build_tool_units() {
  local g="${GRADLE_ROOT:-$HOME/.gradle}" m="${MAVEN_ROOT:-$HOME/.m2}"
  [[ -d "$g/caches"  ]] && register_unit gradle-caches  3 1 "gradle caches"  paths "$g/caches"  "--gradle"
  [[ -d "$g/wrapper" ]] && register_unit gradle-wrapper 3 1 "gradle wrapper" paths "$g/wrapper" "--gradle"
  [[ -d "$g/daemon"  ]] && register_unit gradle-daemon  1 1 "gradle daemon logs" paths "$g/daemon"
  [[ -d "$m/repository" ]] && register_unit maven-repo  3 1 "maven repository" paths "$m/repository" "--maven"
  return 0
}

register_extra_pkg_units() {
  local h="${PKG_HOME:-$HOME}"
  [[ -d "$h/.deno"           ]] && register_unit deno-cache  1 1 "deno cache"  paths "$h/.deno"
  [[ -d "$h/.nuget/packages" ]] && register_unit nuget-cache 1 1 "nuget packages" paths "$h/.nuget/packages"
  [[ -d "$h/.gem"            ]] && register_unit gem-cache   1 1 "gem cache"   paths "$h/.gem"
  return 0
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

  (( ${#rows[@]} )) || return 0
  readarray -t rows < <(printf '%s\n' "${rows[@]}" | sort -k1,1n -k2,2nr)
  for id in "${rows[@]}"; do
    [[ -z "$id" ]] && continue
    SELECTED+=("${id##* }")
  done
}

# ---------------------------------------------------------------------------------
# executor
# ---------------------------------------------------------------------------------
STOPPED_EARLY=0
UNRUN=()
DECLINED=()

execute_plan() {
  STOPPED_EARLY=0; UNRUN=(); DECLINED=()
  local id freed i=0

  for id in "${SELECTED[@]}"; do
    i=$((i + 1))
    if [[ $APPLY -eq 1 ]] && target_met; then
      STOPPED_EARLY=1
      UNRUN=("${SELECTED[@]:$((i - 1))}")
      break
    fi

    if [[ ${U_BYTES[$id]} -eq 0 && "${U_KIND[$id]}" != cmd ]]; then continue; fi

    # --allow-lossy opens the gate; it does not walk through it unattended.
    # Anything that cannot be regenerated is confirmed one unit at a time.
    if [[ $APPLY -eq 1 && "${U_REV[$id]}" != 1 ]]; then
      if ! confirm "delete ${U_LABEL[$id]} ($(human "${U_BYTES[$id]}")) — this cannot be undone?"; then
        DECLINED+=("$id")
        printf '  %sskip%s  %s — declined\n' "$C_YEL" "$C_RESET" "${U_LABEL[$id]}"
        continue
      fi
    fi

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
  return 0
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
  return 0
}

report_heavyweights() {
  (( ${#HEAVY_LIST[@]} )) || return 0
  section "Large directories no rule covers"
  local row b p cls
  while IFS= read -r row; do
    [[ -z "$row" ]] && continue
    b="${row%%$'\t'*}"; p="${row#*$'\t'}"; cls="${p##*$'\t'}"; p="${p%%$'\t'*}"
    # \~ , not ~ : an unescaped tilde in the replacement is tilde-expanded back
    # to $HOME, which makes the whole substitution a no-op
    printf '  %s%8s%s  %-46s %s%s%s\n' "$C_B" "$(human "$b")" "$C_RESET" \
      "${p/#$HOME/\~}" "$C_DIM" "$cls" "$C_RESET"
  done < <(printf '%s\n' "${HEAVY_LIST[@]}" | sort -rn)
  printf '\n  %snothing above was deleted — review these yourself%s\n' "$C_DIM" "$C_RESET"
  return 0
}

report_summary() {
  section "Summary"
  local avail; avail=$(avail_bytes "$TARGET_PATH")
  if [[ $APPLY -eq 1 ]]; then
    printf '  %sFreed this run: %s%s\n' "$C_GRN$C_B" "$(human "$TOTAL_BYTES")" "$C_RESET"
    printf '  Free on %s: %s\n' "$TARGET_MOUNT" "$(human "$avail")"
    (( ${#DECLINED[@]} )) && printf '  %s%d step(s) declined at the prompt%s\n' \
      "$C_DIM" "${#DECLINED[@]}" "$C_RESET"
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
  return 0
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
    '') echo $(( 10#$n )) ;;
    K)  echo $(( 10#$n * 1024 )) ;;
    M)  echo $(( 10#$n * 1024 * 1024 )) ;;
    G)  echo $(( 10#$n * 1024 * 1024 * 1024 )) ;;
    T)  echo $(( 10#$n * 1024 * 1024 * 1024 * 1024 )) ;;
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

sttest_harness() {
  st_assert "human() formats bytes"     "$(human 1048576)" "1.0MiB"
  st_assert "human() handles zero"      "$(human 0)"       "0B"
  local root; root=$(st_fixture)
  st_assert "fixture root exists"       "$([[ -d "$root" ]] && echo yes)" "yes"
  st_assert "fixture npm blob is 1M"    "$(path_bytes "$root/.cache/npm/blob")" "1048576"
  st_assert "fixture protected dir made" \
    "$([[ -d "$root/.config/FakeApp/Local Storage" ]] && echo yes)" "yes"
}

sttest_mounts() {
  st_assert "parse_size plain bytes"  "$(parse_size 1024)"  "1024"
  st_assert "parse_size K"            "$(parse_size 4K)"    "4096"
  st_assert "parse_size M"            "$(parse_size 2M)"    "2097152"
  st_assert "parse_size G"            "$(parse_size 12G)"   "12884901888"
  st_assert "parse_size lowercase g"  "$(parse_size 12g)"   "12884901888"
  st_assert "parse_size leading zero is decimal" "$(parse_size 010G)" "10737418240"
  st_assert "parse_size 09 is decimal not octal" "$(parse_size 09M)"  "9437184"
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

# Reset the registry to empty. Every registry-touching test starts here so the
# tests cannot leak units into each other.
st_reset_registry() {
  U_IDS=(); unset U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED U_MOUNTHINT
  declare -gA U_TIER U_REV U_LABEL U_KIND U_PAYLOAD U_FLAG U_MOUNT U_BYTES U_LOCKED U_MOUNTHINT
}

sttest_registry() {
  local root; root=$(st_fixture)
  st_reset_registry

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
  st_reset_registry

  register_unit "locked-one"   3 1 "locked"   paths "$root/.config/FakeApp/Cache"
  register_unit "locked-two"   3 1 "locked2"  paths "$root/.config/FakeApp/IndexedDB"
  register_unit "unlocked-one" 1 1 "unlocked" paths "$root/.cache/npm"

  # inject a fake running app owning the FakeApp root
  local -a saved_rx=("${LOCK_RX[@]}") saved_roots=("${LOCK_ROOTS[@]}") saved_name=("${LOCK_NAME[@]}")
  LOCK_RX=("fakeapp") ; LOCK_ROOTS=("$root/.config/FakeApp") ; LOCK_NAME=("FakeApp")
  ST_FAKE_PROCS=$'4242\t/usr/bin/fakeapp --no-sandbox'
  apply_locks

  st_assert "unit under locked root is marked" "${U_LOCKED[locked-one]}"   "FakeApp:4242"
  st_assert "EVERY unit under a locked root is marked" "${U_LOCKED[locked-two]}" "FakeApp:4242"
  st_assert "unit outside locked root is free" "${U_LOCKED[unlocked-one]}" ""
  ST_FAKE_PROCS=""
  LOCK_RX=("${saved_rx[@]}"); LOCK_ROOTS=("${saved_roots[@]}"); LOCK_NAME=("${saved_name[@]}")
}

sttest_builtin_units() {
  local root; root=$(st_fixture)
  st_reset_registry

  # Point the machine-dependent scans at the fixture, so the tier assertions do
  # not depend on whether this particular box happens to have stale /tmp entries
  # or month-old transcripts lying around.
  local saved_claude="$CLAUDE_ROOT" saved_tmp="$TMP_SWEEP_ROOT"
  CLAUDE_ROOT="$root/claude"; TMP_SWEEP_ROOT="$root/tmp"
  mkdir -p "$CLAUDE_ROOT/projects/proj" "$TMP_SWEEP_ROOT"
  truncate -s 1M "$CLAUDE_ROOT/projects/proj/old-session.jsonl"
  touch -d '400 days ago' "$CLAUDE_ROOT/projects/proj/old-session.jsonl"
  truncate -s 1M "$TMP_SWEEP_ROOT/leftover.bin"
  touch -d '2 days ago' "$TMP_SWEEP_ROOT/leftover.bin"

  register_all_units
  CLAUDE_ROOT="$saved_claude"; TMP_SWEEP_ROOT="$saved_tmp"

  st_assert_ne "units were registered" "${#U_IDS[@]}" "0"

  # tier assignment matches the spec
  st_assert "npm cache is tier 1"        "${U_TIER[npm-cacache]}"      "1"
  st_assert "playwright is tier 2"       "${U_TIER[playwright]}"       "2"
  st_assert "jetbrains cache is tier 3"  "${U_TIER[jetbrains-cache]}"  "3"
  st_assert "claude history is tier 4"   "${U_TIER[claude-history]}"   "4"
  st_assert "docker volumes are tier 5"  "${U_TIER[docker-volumes]}"   "5"
  st_assert "tmp sweep is tier 0"        "${U_TIER[tmp-stale]}"        "0"

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

  # --system frees space on / only, so it must be tagged to that mount or the
  # planner would offer it while /home is the partition under pressure
  st_assert "apt unit keeps --system flag" "${U_FLAG[system-apt]}" "--system"
  probe_unit system-apt
  st_assert "apt unit is tagged to /"      "${U_MOUNT[system-apt]}" "/"

  # no duplicate ids
  local dupes
  dupes=$(printf '%s\n' "${U_IDS[@]}" | sort | uniq -d | wc -l)
  st_assert "no duplicate unit ids" "$dupes" "0"

  # every unit must carry a non-empty label and a known kind
  local badmeta=0
  for id in "${U_IDS[@]}"; do
    [[ -z "${U_LABEL[$id]}" ]] && badmeta=1
    case "${U_KIND[$id]}" in paths|cmd) ;; *) badmeta=1 ;; esac
  done
  st_assert "every unit has a label and valid kind" "$badmeta" "0"
}

sttest_sites_idle_units() {
  local root; root=$(st_fixture)
  st_reset_registry

  # a dormant project with deps beside its manifest, and an active one
  mkdir -p "$root/sites/dormant/node_modules" "$root/sites/active/node_modules"
  touch "$root/sites/dormant/package.json" "$root/sites/active/package.json"
  truncate -s 2M "$root/sites/dormant/node_modules/lib.js"
  truncate -s 2M "$root/sites/active/node_modules/lib.js"
  # backdate the dormant project's sources well past the idle threshold
  touch -d '400 days ago' "$root/sites/dormant/package.json"

  SITES_IDLE_DAYS=30 SITES_ROOT="$root/sites"
  register_sites_idle_units
  SITES_IDLE_DAYS=0

  local dormant=0 active=0 id
  for id in "${U_IDS[@]}"; do
    [[ "${U_PAYLOAD[$id]}" == *"/dormant/"* ]] && dormant=1
    [[ "${U_PAYLOAD[$id]}" == *"/active/"*  ]] && active=1
  done
  st_assert "dormant site deps registered"   "$dormant" "1"
  st_assert "ACTIVE site deps left alone"    "$active"  "0"
  for id in "${U_IDS[@]}"; do
    st_assert "sites-idle unit $id is lossy tier 4" "${U_TIER[$id]}/${U_REV[$id]}" "4/0"
  done
}

sttest_build_tool_units() {
  local root; root=$(st_fixture)
  st_reset_registry

  mkdir -p "$root/.gradle/caches/modules-2" "$root/.gradle/wrapper/dists" \
           "$root/.gradle/daemon" "$root/.m2/repository/org"
  truncate -s 3M "$root/.gradle/caches/modules-2/lib.jar"
  truncate -s 1M "$root/.gradle/wrapper/dists/gradle.zip"
  truncate -s 2M "$root/.m2/repository/org/lib.jar"

  GRADLE_ROOT="$root/.gradle" MAVEN_ROOT="$root/.m2" register_build_tool_units

  local g_cache=0 g_wrap=0 m_repo=0 id
  for id in "${U_IDS[@]}"; do
    [[ "$id" == gradle-caches  ]] && g_cache=1
    [[ "$id" == gradle-wrapper ]] && g_wrap=1
    [[ "$id" == maven-repo     ]] && m_repo=1
  done
  st_assert "gradle caches registered"  "$g_cache" "1"
  st_assert "gradle wrapper registered" "$g_wrap"  "1"
  st_assert "maven repo registered"     "$m_repo"  "1"

  # regenerable, but a cold re-download — same tier as a JetBrains reindex
  st_assert "gradle caches reversible tier 3" \
    "${U_TIER[gradle-caches]:-}/${U_REV[gradle-caches]:-}" "3/1"
  st_assert "maven repo reversible tier 3" \
    "${U_TIER[maven-repo]:-}/${U_REV[maven-repo]:-}" "3/1"
  st_assert "gradle caches opt-in flag" "${U_FLAG[gradle-caches]:-}" "--gradle"
  st_assert "maven repo opt-in flag"    "${U_FLAG[maven-repo]:-}"    "--maven"
  st_assert "gradle caches claims caches dir" \
    "${U_PAYLOAD[gradle-caches]:-}" "$root/.gradle/caches"

  # a build tool that is not installed must contribute nothing
  st_reset_registry
  GRADLE_ROOT="$root/absent" MAVEN_ROOT="$root/absent" register_build_tool_units
  st_assert "absent build tools register nothing" "${#U_IDS[@]}" "0"
}

sttest_gradle_lock() {
  # a running Gradle daemon or Android Studio must park the gradle units
  local hit=0 i
  for i in "${!LOCK_RX[@]}"; do
    [[ "${LOCK_NAME[$i]}" == "gradle" ]] && hit=1
  done
  st_assert "gradle has a lock rule" "$hit" "1"

  local matched=0 i
  for i in "${!LOCK_RX[@]}"; do
    [[ "${LOCK_NAME[$i]}" == "gradle" ]] || continue
    [[ "java -cp /home/u/.gradle/wrapper/dists GradleDaemon 8.5" =~ ${LOCK_RX[$i]} ]] && matched=1
  done
  st_assert "GradleDaemon command line matches the gradle lock" "$matched" "1"
}

sttest_cache_name_case() {
  # XDG and most Linux apps use lowercase; the whitelist was Chromium-cased only
  st_assert "lowercase cache matched"  "$(is_cache_name 'cache'    && echo y || echo n)" "y"
  st_assert "uppercase CACHE matched"  "$(is_cache_name 'CACHE'    && echo y || echo n)" "y"
  st_assert "mixed GpuCache matched"   "$(is_cache_name 'GpuCache' && echo y || echo n)" "y"
  # protection must stay case-insensitive too, or lowercase state dirs get eaten
  st_assert "lowercase local storage PROTECTED" \
    "$(is_cache_name 'local storage' && echo y || echo n)" "n"
  st_assert "lowercase cookies PROTECTED" \
    "$(is_cache_name 'cookies' && echo y || echo n)" "n"
  st_assert "uppercase INDEXEDDB PROTECTED" \
    "$(is_cache_name 'INDEXEDDB' && echo y || echo n)" "n"
}

sttest_xdg_cache_discovery() {
  local root; root=$(st_fixture)
  st_reset_registry

  mkdir -p "$root/.cache/randomtool" "$root/.cache/npm"
  truncate -s 2M "$root/.cache/randomtool/blob"

  # a hardcoded unit already owns .cache/npm — discovery must not claim it twice
  register_unit npm-cacache 1 1 "npm cache" paths "$root/.cache/npm"

  XDG_CACHE_ROOT="$root/.cache" discover_xdg_caches

  local found_random=0 npm_seen=0 id
  for id in "${U_IDS[@]}"; do
    [[ "${U_PAYLOAD[$id]}" == "$root/.cache/randomtool" ]] && found_random=1
    [[ "${U_PAYLOAD[$id]}" == "$root/.cache/npm" ]] && npm_seen=$((npm_seen + 1))
  done
  st_assert "unclaimed ~/.cache child discovered"    "$found_random" "1"
  st_assert "claimed ~/.cache child not re-claimed"  "$npm_seen"     "1"

  for id in "${U_IDS[@]}"; do
    [[ "${U_PAYLOAD[$id]}" == "$root/.cache/randomtool" ]] || continue
    st_assert "discovered xdg cache is reversible tier 2" \
      "${U_TIER[$id]}/${U_REV[$id]}" "2/1"
  done
}

sttest_extra_pkg_units() {
  local root; root=$(st_fixture)
  st_reset_registry

  mkdir -p "$root/.deno" "$root/.nuget/packages" "$root/.gem"
  truncate -s 1M "$root/.deno/dep" "$root/.nuget/packages/dep" "$root/.gem/dep"

  PKG_HOME="$root" register_extra_pkg_units

  local deno=0 nuget=0 gem=0 id
  for id in "${U_IDS[@]}"; do
    [[ "$id" == deno-cache  ]] && deno=1
    [[ "$id" == nuget-cache ]] && nuget=1
    [[ "$id" == gem-cache   ]] && gem=1
  done
  st_assert "deno cache registered"  "$deno"  "1"
  st_assert "nuget cache registered" "$nuget" "1"
  st_assert "gem cache registered"   "$gem"   "1"
  st_assert "deno cache is reversible tier 1" \
    "${U_TIER[deno-cache]:-}/${U_REV[deno-cache]:-}" "1/1"
}

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
  st_assert "Cookies PROTECTED"        "$(is_cache_name 'Cookies' && echo y || echo n)"        "n"
  st_assert "Login Data PROTECTED"     "$(is_cache_name 'Login Data' && echo y || echo n)"     "n"
  st_assert "unrelated name unmatched" "$(is_cache_name 'Extensions' && echo y || echo n)"     "n"

  # end-to-end against the fixture
  local root; root=$(st_fixture)
  st_reset_registry

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
  DISCOVER_ROOTS=("$HOME/.config" "$HOME/.local/share" "$HOME/.cache")
}

sttest_heavyweights() {
  local root; root=$(st_fixture)
  mkdir -p "$root/BigData"
  truncate -s 300M "$root/BigData/blob.bin"
  mkdir -p "$root/SmallThing"
  truncate -s 1M   "$root/SmallThing/blob.bin"

  st_reset_registry
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
  HEAVY_LIST=(); HEAVY_ROOTS=("$HOME")
}

sttest_planner() {
  local root; root=$(st_fixture)
  st_reset_registry

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

  # an empty registry must not blow up on set -u
  st_reset_registry
  select_units
  st_assert "empty registry selects nothing" "${#SELECTED[@]}" "0"

  # target parsing
  TARGET_BYTES=""; FREE_ARG="12G"; TARGET_PATH="$HOME"
  resolve_target
  st_assert "resolve_target parses --free" "$TARGET_BYTES" "12884901888"
  FREE_ARG="12G:$root"
  resolve_target
  st_assert "resolve_target honours :path" "$TARGET_PATH" "$root"
  FREE_ARG=""; TARGET_BYTES=""; TARGET_PATH="$HOME"
}

sttest_executor() {
  local root; root=$(st_fixture)
  st_reset_registry

  register_unit e1 1 1 "e1" paths "$root/.cache/npm/blob"
  register_unit e2 2 1 "e2" paths "$root/.config/FakeApp/Cache"
  local id; for id in "${U_IDS[@]}"; do probe_unit "$id"; done
  SELECTED=(e1 e2)

  # dry-run must delete nothing
  APPLY=0 ASSUME_YES=1 TARGET_BYTES="" TOTAL_BYTES=0
  execute_plan >/dev/null
  st_assert "dry-run deletes nothing (e1)" "$([[ -e "$root/.cache/npm/blob" ]] && echo yes)" "yes"
  st_assert "dry-run deletes nothing (e2)" "$([[ -e "$root/.config/FakeApp/Cache" ]] && echo yes)" "yes"
  st_assert "dry-run still totals bytes"   "$TOTAL_BYTES" "3145728"

  # apply with an already-satisfied target must run nothing at all
  APPLY=1 TARGET_BYTES=1 TARGET_PATH="$root" TOTAL_BYTES=0
  execute_plan >/dev/null
  st_assert "target already met - nothing ran" "$TOTAL_BYTES" "0"
  st_assert "early stop recorded"              "$STOPPED_EARLY" "1"
  st_assert "files survive early stop"         "$([[ -e "$root/.cache/npm/blob" ]] && echo yes)" "yes"

  # apply with an unreachable target must run everything
  APPLY=1 TARGET_BYTES=$(( 1024 ** 5 )) TOTAL_BYTES=0
  execute_plan >/dev/null
  st_assert "unreachable target runs all"  "$TOTAL_BYTES" "3145728"
  st_assert "e1 deleted" "$([[ -e "$root/.cache/npm/blob" ]] && echo yes || echo no)" "no"
  st_assert "e2 deleted" "$([[ -e "$root/.config/FakeApp/Cache" ]] && echo yes || echo no)" "no"
  st_assert "protected fixture dir untouched" \
    "$([[ -e "$root/.config/FakeApp/Local Storage" ]] && echo yes)" "yes"

  # a lossy unit must be confirmed before it runs, even once --allow-lossy
  # opened the gate. Declining at the prompt leaves the data alone.
  st_cleanup; root=$(st_fixture)
  st_reset_registry
  register_unit lossy1 4 0 "lossy1" paths "$root/.config/FakeApp/Local Storage"
  probe_unit lossy1
  SELECTED=(lossy1)
  APPLY=1 ASSUME_YES=0 TARGET_BYTES="" TARGET_PATH="$root" TOTAL_BYTES=0
  execute_plan </dev/null >/dev/null
  st_assert "declined lossy unit is NOT deleted" \
    "$([[ -e "$root/.config/FakeApp/Local Storage" ]] && echo yes || echo no)" "yes"
  st_assert "declined lossy unit freed nothing" "$TOTAL_BYTES" "0"

  # --yes answers the prompt
  APPLY=1 ASSUME_YES=1 TOTAL_BYTES=0
  execute_plan >/dev/null
  st_assert "--yes runs the lossy unit" \
    "$([[ -e "$root/.config/FakeApp/Local Storage" ]] && echo yes || echo no)" "no"
  st_assert "--yes lossy unit freed bytes" "$TOTAL_BYTES" "4194304"

  APPLY=0 ASSUME_YES=0 TARGET_BYTES="" TARGET_PATH="$HOME" SELECTED=()
}

sttest_reporting() {
  local root; root=$(st_fixture)
  st_reset_registry

  register_unit r1 3 1 "jetbrains caches" paths "$root/.config/FakeApp/Cache"
  probe_unit r1
  U_LOCKED[r1]="android-studio:324491"

  local out; out=$(report_locked)
  st_assert "locked section names the app"  "$(printf '%s' "$out" | grep -c 'android-studio')" "1"
  st_assert "locked section shows the pid"  "$(printf '%s' "$out" | grep -c '324491')"        "1"
  # the size shows on the unit's own line and again in the "unlock N" total, so
  # assert presence rather than an exact line count
  st_assert_ne "locked section shows a size" "$(printf '%s' "$out" | grep -c '2.0MiB')"       "0"

  SKIPPED_LOSSY=(r1)
  out=$(report_lossy_withheld)
  st_assert "withheld section suggests the flag" "$(printf '%s' "$out" | grep -c -- '--allow-lossy')" "1"

  HEAVY_LIST=("$(printf '5368709120\t%s/Downloads\tdata' "$root")")
  out=$(report_heavyweights)
  st_assert "heavyweight report shows path"  "$(printf '%s' "$out" | grep -c 'Downloads')" "1"
  st_assert "heavyweight report is advisory" "$(printf '%s' "$out" | grep -ci 'nothing.*deleted')" "1"

  # paths under $HOME abbreviate to ~ — an unescaped tilde in the replacement
  # expands back to $HOME and silently disables this
  HEAVY_LIST=("$(printf '5368709120\t%s/Downloads\tdata' "$HOME")")
  out=$(report_heavyweights)
  st_assert "home paths abbreviate to ~" "$(printf '%s' "$out" | grep -c '~/Downloads')" "1"

  # JSON must be parseable and must not claim to have deleted anything in dry-run
  APPLY=0 TOTAL_BYTES=2097152 TARGET_BYTES="" STOPPED_EARLY=0
  out=$(emit_json)
  st_assert "json has reclaimable key" "$(printf '%s' "$out" | grep -c '"reclaimable_bytes"')" "1"
  st_assert "json marks dry-run"       "$(printf '%s' "$out" | grep -c '"applied": false')"    "1"
  if command -v python3 >/dev/null 2>&1; then
    printf '%s' "$out" | python3 -c 'import json,sys; json.load(sys.stdin)' 2>/dev/null
    st_assert "json parses" "$?" "0"

    # and it must still be valid JSON when no unit has any bytes
    st_reset_registry
    out=$(emit_json)
    printf '%s' "$out" | python3 -c 'import json,sys; json.load(sys.stdin)' 2>/dev/null
    st_assert "json parses with zero units" "$?" "0"
  fi
  SKIPPED_LOSSY=(); HEAVY_LIST=(); TOTAL_BYTES=0
}

# Re-execs this script, so it must never run in a child (st_run skips it when
# CLEANUP_ST_CHILD is set) or the suite would fork without bound.
sttest_cli() {
  local f
  for f in --apply --docker --docker-all --docker-volumes --jetbrains --browsers \
           --playwright --claude-vm --claude-jobs --claude-plugins --claude-all \
           --auto --allow-lossy --discover --json; do
    CLEANUP_ST_CHILD=1 bash "$0" "$f" --self-test >/dev/null 2>&1
    st_assert "flag $f parses" "$?" "0"
  done
  for f in "--deps 30" "--sites-idle 30" "--claude-history 30" "--free 1G" "--tier 2"; do
    # shellcheck disable=SC2086
    CLEANUP_ST_CHILD=1 bash "$0" $f --self-test >/dev/null 2>&1
    st_assert "flag $f parses" "$?" "0"
  done

  CLEANUP_ST_CHILD=1 bash "$0" --nonsense-flag >/dev/null 2>&1
  st_assert "unknown flag still exits 2" "$?" "2"

  CLEANUP_ST_CHILD=1 bash "$0" --free banana >/dev/null 2>&1
  st_assert "bad --free size exits 2" "$?" "2"

  # help must document every new flag
  local h; h=$(bash "$0" --help 2>&1)
  for f in --free --auto --tier --allow-lossy --discover --json --self-test; do
    st_assert_ne "help documents $f" "$(printf '%s' "$h" | grep -c -- "$f")" "0"
  done
  # and must not have lost the legacy ones
  for f in --apply --docker --jetbrains --browsers --playwright --deps --sites-idle \
           --system --claude-vm --claude-jobs --claude-plugins --claude-history --claude-all; do
    st_assert_ne "help still documents $f" "$(printf '%s' "$h" | grep -c -- "$f")" "0"
  done

  # a dry run must be exactly that: no target, nothing deleted, exit 0
  CLEANUP_ST_CHILD=1 bash "$0" >/dev/null 2>&1
  st_assert "bare dry-run exits 0" "$?" "0"
}

confirm() { # confirm "question"  -> 0 yes / 1 no
  [[ $ASSUME_YES -eq 1 ]] && return 0
  [[ $APPLY -eq 0 ]] && return 0   # dry-run always "proceeds" (nothing deleted)
  local a; read -r -p "  ${C_YEL}?${C_RESET} $1 [y/N] " a
  [[ "$a" =~ ^[Yy]$ ]]
}

# ---------------------------------------------------------------------------------
# preflight
# ---------------------------------------------------------------------------------
[[ $EUID -eq 0 ]] && { echo "${C_RED}Refusing to run as root.${C_RESET} Run as your normal user (system steps use sudo as needed)." >&2; exit 1; }

if [[ $SELF_TEST -eq 1 ]]; then
  st_run; exit $?
fi


# ---------------------------------------------------------------------------------
# main: survey -> register -> probe -> lock -> select -> execute -> report
# ---------------------------------------------------------------------------------
main() {
  register_all_units
  [[ $DO_DISCOVER -eq 1 ]] && { discover_app_caches; discover_xdg_caches; }

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
  return 0
}

main
