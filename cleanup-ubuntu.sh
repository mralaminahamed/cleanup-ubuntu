#!/usr/bin/env bash
#
# cleanup-ubuntu.sh — intelligent, process-aware disk cleanup for Ubuntu / Debian
# ---------------------------------------------------------------------------------
# Safe by default. Reclaims space from regenerable caches only. Never deletes
# source code, documents, LLM models, or databases.
#
# HIGHLIGHTS
#   * DRY-RUN by default — shows what WOULD be freed. Use --apply to actually delete.
#   * Process-aware — skips caches of apps that are currently running (IDEs,
#     browsers, Docker) so it never corrupts a live session.
#   * Tool-aware — auto-detects installed package managers and uses their native
#     "cache clean" commands where possible (safer than blind rm).
#   * Protects real data — Downloads, source trees, ollama models, MEGA/Dropbox
#     syncs, and Docker *named* volumes are never touched.
#
# USAGE
#   ./cleanup-ubuntu.sh                 # dry-run, show reclaimable space
#   ./cleanup-ubuntu.sh --apply         # actually clean (asks before big/risky steps)
#   ./cleanup-ubuntu.sh --apply --yes   # non-interactive
#
# OPT-IN EXTRAS (all off by default)
#   --docker            prune unused Docker images/containers/build-cache/networks
#   --docker-all        also remove ALL unused images (not just dangling)
#   --docker-volumes    also prune unused Docker volumes  (⚠ may delete DB data)
#   --jetbrains         clear JetBrains IDE caches (only if no JetBrains proc runs)
#   --browsers          clear browser HTTP caches (only for browsers not running)
#   --playwright        remove Playwright browser binaries (~/.cache/ms-playwright)
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

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply)          APPLY=1 ;;
    -y|--yes)         ASSUME_YES=1 ;;
    --docker)         DO_DOCKER=1 ;;
    --docker-all)     DO_DOCKER=1; DOCKER_ALL=1 ;;
    --docker-volumes) DO_DOCKER=1; DOCKER_VOLUMES=1 ;;
    --jetbrains)      DO_JETBRAINS=1 ;;
    --browsers)       DO_BROWSERS=1 ;;
    --playwright)     DO_PLAYWRIGHT=1 ;;
    --deps|--node-modules) DEPS_DAYS="${2:-30}"; shift ;;
    --sites-idle)     SITES_IDLE_DAYS="${2:-30}"; shift
                      # optional DIR arg: consume it only if it's an existing dir
                      if [[ -n "${2:-}" && -d "${2}" ]]; then SITES_ROOT="$2"; shift; fi ;;
    --system)         DO_SYSTEM=1 ;;
    --claude-vm)      DO_CLAUDE_VM=1 ;;
    --claude-jobs)    DO_CLAUDE_JOBS=1
                      # optional DAYS arg: consume it only if it really is a number
                      if [[ "${2:-}" =~ ^[0-9]+$ ]]; then CLAUDE_JOBS_DAYS="$2"; shift; fi ;;
    --claude-plugins) DO_CLAUDE_PLUGINS=1 ;;
    --claude-history) CLAUDE_HISTORY_DAYS="${2:-30}"
                      [[ "${2:-}" =~ ^[0-9]+$ ]] && shift ;;
    --claude-all)     DO_CLAUDE_JOBS=1; DO_CLAUDE_PLUGINS=1 ;;
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

# is a process matching regex running?
is_running() { pgrep -af "$1" 2>/dev/null | grep -qvE 'pgrep|cleanup-ubuntu'; }

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

sttest_harness() {
  st_assert "human() formats bytes"     "$(human 1048576)" "1.0MiB"
  st_assert "human() handles zero"      "$(human 0)"       "0B"
  local root; root=$(st_fixture)
  st_assert "fixture root exists"       "$([[ -d "$root" ]] && echo yes)" "yes"
  st_assert "fixture npm blob is 1M"    "$(path_bytes "$root/.cache/npm/blob")" "1048576"
  st_assert "fixture protected dir made" \
    "$([[ -d "$root/.config/FakeApp/Local Storage" ]] && echo yes)" "yes"
}

# ---------------------------------------------------------------------------------
# core: report + (optionally) delete a path. Adds to TOTAL_BYTES when acted on.
#   reclaim <label> <path...>
# ---------------------------------------------------------------------------------
reclaim() {
  local label="$1"; shift
  local total=0 p b
  for p in "$@"; do
    [[ -e "$p" ]] || continue
    b=$(path_bytes "$p"); total=$((total + b))
  done
  [[ $total -eq 0 ]] && return 0
  if [[ $APPLY -eq 1 ]]; then
    for p in "$@"; do [[ -e "$p" ]] && chmod -R u+w "$p" 2>/dev/null; rm -rf -- "$p" 2>/dev/null; done
    printf '  %s✓%s %-34s %s%s%s freed\n' "$C_GRN" "$C_RESET" "$label" "$C_GRN" "$(human "$total")" "$C_RESET"
  else
    printf '  %s•%s %-34s %s%s%s reclaimable\n' "$C_CYN" "$C_RESET" "$label" "$C_B" "$(human "$total")" "$C_RESET"
  fi
  TOTAL_BYTES=$((TOTAL_BYTES + total))
}

# run a native cache-clean command (only when --apply); report is best-effort.
run_clean() { # run_clean <label> <cmd...>
  local label="$1"; shift
  command -v "$1" >/dev/null 2>&1 || return 0
  if [[ $APPLY -eq 1 ]]; then
    "$@" >/dev/null 2>&1 && printf '  %s✓%s %s\n' "$C_GRN" "$C_RESET" "$label (native clean)"
  else
    printf '  %s•%s %s\n' "$C_CYN" "$C_RESET" "$label — would run: $*"
  fi
}

# reclaim direct children of <dir> not modified in <days> days.
#   reclaim_stale <label> <dir> <days> [extra find predicates...]
reclaim_stale() {
  local label="$1" dir="$2" days="$3"; shift 3
  [[ -d "$dir" ]] || return 0
  local -a hits=()
  mapfile -t hits < <(find "$dir" -mindepth 1 -maxdepth 1 -mtime "+$days" "$@" 2>/dev/null)
  (( ${#hits[@]} )) || return 0
  reclaim "$label (${#hits[@]})" "${hits[@]}"
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

HOME_FS=$(df -P "$HOME" | awk 'NR==2{print $1}')
before_avail=$(df -P "$HOME" | awk 'NR==2{print $4}')

printf '%s%s cleanup-ubuntu %s  user=%s  host=%s\n' "$C_B" "$C_CYN" "$C_RESET" "$USER" "$(hostname)"
if [[ $APPLY -eq 0 ]]; then
  printf '%sDRY-RUN%s — nothing will be deleted. Re-run with %s--apply%s to clean.\n' "$C_YEL" "$C_RESET" "$C_B" "$C_RESET"
else
  printf '%sAPPLY MODE%s — caches will be deleted.\n' "$C_RED" "$C_RESET"
fi
df -h "$HOME" | awk 'NR==1||NR==2'

# ---------------------------------------------------------------------------------
# 1. JavaScript / Node package-manager caches
# ---------------------------------------------------------------------------------
section "JS / Node package caches"
run_clean "npm"        npm cache clean --force
run_clean "pnpm store" pnpm store prune
run_clean "yarn"       yarn cache clean
command -v bun >/dev/null 2>&1 && run_clean "bun" bun pm cache rm
reclaim "npm _cacache"      "$HOME/.npm/_cacache"
reclaim "npm _npx"          "$HOME/.npm/_npx"
reclaim "npm _logs"         "$HOME/.npm/_logs"
reclaim "yarn (classic)"    "$HOME/.cache/yarn"
reclaim "yarn berry cache"  "$HOME/.yarn/berry/cache"
reclaim "pnpm cache"        "$HOME/.cache/pnpm"
reclaim "pnpm store"        "$HOME/.local/share/pnpm/store"
reclaim "bun cache"         "$HOME/.bun/install/cache"
reclaim "node-gyp headers"  "$HOME/.cache/node-gyp"

# ---------------------------------------------------------------------------------
# 2. Other language / toolchain caches
# ---------------------------------------------------------------------------------
section "Other language caches"
run_clean "go build+mod" go clean -cache -modcache
run_clean "composer"     composer clear-cache
run_clean "pip"          pip cache purge
run_clean "uv"           uv cache clean
if command -v cargo-cache >/dev/null 2>&1; then
  run_clean "cargo" cargo-cache -a
else
  reclaim "cargo registry cache" "$HOME/.cargo/registry/cache" "$HOME/.cargo/registry/src"
fi
reclaim "go build cache"  "$HOME/.cache/go-build"
reclaim "go module cache" "$HOME/go/pkg/mod"    # go clean handles perms; this catches leftovers
reclaim "composer cache"  "$HOME/.cache/composer"
reclaim "uv cache"        "$HOME/.cache/uv"
reclaim "pip cache"       "$HOME/.cache/pip"

# ---------------------------------------------------------------------------------
# 3. Dev-tool / misc app caches (regenerable)
# ---------------------------------------------------------------------------------
section "Dev-tool & misc caches"
reclaim "phpactor index"     "$HOME/.cache/phpactor"
reclaim "chrome-devtools-mcp" "$HOME/.cache/chrome-devtools-mcp"
reclaim "act (gh actions)"   "$HOME/.cache/act"
reclaim "giget templates"    "$HOME/.cache/giget"
reclaim "thumbnails"         "$HOME/.cache/thumbnails"
reclaim "Trash"              "$HOME/.local/share/Trash/files" "$HOME/.local/share/Trash/info"

# ---------------------------------------------------------------------------------
# 4. Playwright browser binaries (opt-in — you must reinstall to use Playwright)
# ---------------------------------------------------------------------------------
if [[ $DO_PLAYWRIGHT -eq 1 ]]; then
  section "Playwright browsers"
  info "after this, run: npx playwright install  (to restore)"
  reclaim "ms-playwright browsers" "$HOME/.cache/ms-playwright"
fi

# ---------------------------------------------------------------------------------
# 5. Browser HTTP caches — ONLY for browsers that are NOT running
#    (clears the disk cache subdirs only; never profiles/history/passwords)
# ---------------------------------------------------------------------------------
if [[ $DO_BROWSERS -eq 1 ]]; then
  section "Browser HTTP caches (idle browsers only)"
  clear_browser_cache() { # <name> <proc-regex> <config-root>
    local name="$1" rx="$2" root="$3"
    [[ -d "$root" ]] || return 0
    if is_running "$rx"; then skip "$name is running — leaving its cache"; return 0; fi
    # chromium-family: <profile>/Cache, Code Cache, GPUCache ; firefox: cache2 under ~/.cache
    reclaim "$name cache" \
      "$root"/*/Cache "$root"/*/"Code Cache" "$root"/*/GPUCache \
      "$root"/Default/Cache "$root"/Default/"Code Cache"
  }
  clear_browser_cache "Google Chrome" 'chrome'  "$HOME/.config/google-chrome"
  clear_browser_cache "Brave"         'brave'   "$HOME/.config/BraveSoftware/Brave-Browser"
  # chromium HTTP caches under ~/.cache too
  is_running 'chrome' || reclaim "chrome ~/.cache"  "$HOME/.cache/google-chrome"
  is_running 'brave'  || reclaim "brave ~/.cache"   "$HOME/.cache/BraveSoftware"
  is_running 'firefox' || reclaim "firefox ~/.cache" "$HOME/.cache/mozilla"
fi

# ---------------------------------------------------------------------------------
# 6. JetBrains IDE caches — only if no JetBrains process is running
# ---------------------------------------------------------------------------------
if [[ $DO_JETBRAINS -eq 1 ]]; then
  section "JetBrains caches"
  if is_running 'jetbrains|idea|pycharm|phpstorm|webstorm|goland|clion|rider|rubymine|datagrip'; then
    skip "a JetBrains IDE is running — close it first (would corrupt indexes)"
  elif confirm "clear JetBrains caches (forces reindex on next open)?"; then
    reclaim "JetBrains cache" "$HOME/.cache/JetBrains"
  fi
fi

# ---------------------------------------------------------------------------------
# 7. /tmp user leftovers — protect live infrastructure
# ---------------------------------------------------------------------------------
section "/tmp user leftovers"
# Protected name patterns: things a running session/desktop needs.
tmp_protected='^(claude-|cc-daemon|claude-mcp|\.X|\.ICE|\.XIM|\.font|com\.google\.Chrome|\.com\.google|org\.chromium|\.org\.chromium|scoped_dir|hsperfdata|systemd-private|snap-private|\.Test-unix|cef_server)'
shopt -s nullglob dotglob
tmp_freed=0
for entry in /tmp/*; do
  name=$(basename "$entry")
  # only our own files
  [[ "$(stat -c '%U' "$entry" 2>/dev/null)" == "$USER" ]] || { continue; }
  if [[ "$name" =~ $tmp_protected ]]; then continue; fi
  # only stale: not modified in last 60 min (avoids nuking in-flight temp)
  if [[ -n "$(find "$entry" -maxdepth 0 -mmin -60 2>/dev/null)" ]]; then continue; fi
  b=$(path_bytes "$entry"); tmp_freed=$((tmp_freed + b))
  [[ $APPLY -eq 1 ]] && { chmod -R u+w "$entry" 2>/dev/null; rm -rf -- "$entry" 2>/dev/null; }
done
shopt -u nullglob dotglob
if [[ $tmp_freed -gt 0 ]]; then
  if [[ $APPLY -eq 1 ]]; then printf '  %s✓%s stale /tmp files              %s%s freed\n' "$C_GRN" "$C_RESET" "$C_GRN" "$(human "$tmp_freed")$C_RESET"
  else printf '  %s•%s stale /tmp files              %s%s reclaimable\n' "$C_CYN" "$C_RESET" "$C_B" "$(human "$tmp_freed")$C_RESET"; fi
  TOTAL_BYTES=$((TOTAL_BYTES + tmp_freed))
else info "nothing stale"; fi
kept "live infra (claude/X11/chrome/systemd/…) protected"

# ---------------------------------------------------------------------------------
# 8. Docker — only unused resources; never named volumes unless asked
# ---------------------------------------------------------------------------------
if [[ $DO_DOCKER -eq 1 ]]; then
  section "Docker"
  if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
    skip "docker not available / daemon not reachable"
  else
    docker system df 2>/dev/null | sed 's/^/  /'
    if confirm "prune unused Docker data?"; then
      if [[ $APPLY -eq 1 ]]; then
        docker builder prune -f     >/dev/null 2>&1 && info "build cache pruned"
        docker container prune -f    >/dev/null 2>&1 && info "stopped containers pruned"
        docker network prune -f      >/dev/null 2>&1 && info "unused networks pruned"
        if [[ $DOCKER_ALL -eq 1 ]]; then docker image prune -af >/dev/null 2>&1 && info "ALL unused images pruned"
        else docker image prune -f  >/dev/null 2>&1 && info "dangling images pruned"; fi
        if [[ $DOCKER_VOLUMES -eq 1 ]]; then
          printf '  %s⚠ pruning unused volumes — this can delete database data%s\n' "$C_RED" "$C_RESET"
          docker volume prune -f >/dev/null 2>&1 && info "unused volumes pruned"
        else kept "named volumes preserved (use --docker-volumes to prune)"; fi
      else
        info "would prune: build cache, stopped containers, unused networks$([[ $DOCKER_ALL -eq 1 ]] && echo ', ALL unused images' || echo ', dangling images')$([[ $DOCKER_VOLUMES -eq 1 ]] && echo ', unused volumes')"
        kept "named volumes preserved unless --docker-volumes"
      fi
    fi
  fi
fi

# ---------------------------------------------------------------------------------
# 9. Stale dependency dirs — node_modules + composer vendor (opt-in, age-filtered)
#    A dir is only removed when its package manifest sits beside it, so unrelated
#    directories that happen to be named "vendor"/"node_modules" are never touched.
# ---------------------------------------------------------------------------------
if [[ ${DEPS_DAYS:-0} -gt 0 ]]; then
  section "Stale dependency dirs (> ${DEPS_DAYS}d untouched)"
  info "searching ~/Projects, ~/Sites, ~/code, ~/dev … (restore with npm/composer install)"
  DEPS_ROOTS=()
  for r in "$HOME/Projects" "$HOME/Sites" "$HOME/code" "$HOME/dev"; do [[ -d "$r" ]] && DEPS_ROOTS+=("$r"); done
  if [[ ${#DEPS_ROOTS[@]} -eq 0 ]]; then
    skip "no project roots found"
  else
    # node_modules — keep only if a package.json is its sibling
    while IFS= read -r nm; do
      [[ -z "$nm" ]] && continue
      [[ -f "$(dirname "$nm")/package.json" ]] || continue
      reclaim "nm  $(dirname "$nm" | sed "s#$HOME#~#")" "$nm"
    done < <(find "${DEPS_ROOTS[@]}" -maxdepth 6 -type d -name node_modules -prune -mtime "+$DEPS_DAYS" 2>/dev/null)
    # composer vendor — only when a composer.json is its sibling
    while IFS= read -r vd; do
      [[ -z "$vd" ]] && continue
      [[ -f "$(dirname "$vd")/composer.json" ]] || continue
      reclaim "vendor  $(dirname "$vd" | sed "s#$HOME#~#")" "$vd"
    done < <(find "${DEPS_ROOTS[@]}" -maxdepth 6 -type d -name vendor -prune -mtime "+$DEPS_DAYS" 2>/dev/null)
  fi
fi

# ---------------------------------------------------------------------------------
# 9b. Dormant-site deps — clear node_modules/vendor only in projects whose newest
#     source file (ignoring .git/node_modules/vendor) is older than DAYS. This
#     protects sites you touched recently even if their deps look old, and clears
#     idle sites even if a stray file inside deps has a fresh timestamp.
# ---------------------------------------------------------------------------------
if [[ ${SITES_IDLE_DAYS:-0} -gt 0 ]]; then
  section "Dormant-site deps (idle > ${SITES_IDLE_DAYS}d in ${SITES_ROOT/#$HOME/~})"
  if [[ ! -d "$SITES_ROOT" ]]; then
    skip "no such dir: $SITES_ROOT"
  else
    now_ts=$(date +%s)
    for site in "$SITES_ROOT"/*/; do
      site=${site%/}
      [[ -d "$site" ]] || continue
      newest=$(find "$site" -type f -not -path '*/.git/*' -not -path '*/node_modules/*' -not -path '*/vendor/*' \
                 -printf '%T@\n' 2>/dev/null | sort -rn | head -1)
      newest=${newest%.*}; [[ -z "$newest" ]] && newest=0
      idle=$(( (now_ts - newest) / 86400 ))
      if (( idle <= SITES_IDLE_DAYS )); then
        kept "$(basename "$site") — active (${idle}d idle)"
        continue
      fi
      found=0
      while IFS= read -r nm; do
        [[ -z "$nm" ]] && continue
        [[ -f "$(dirname "$nm")/package.json" ]] || continue
        found=1; reclaim "$(basename "$site")/…/node_modules" "$nm"
      done < <(find "$site" -maxdepth 6 -type d -name node_modules -prune 2>/dev/null)
      while IFS= read -r vd; do
        [[ -z "$vd" ]] && continue
        [[ -f "$(dirname "$vd")/composer.json" ]] || continue
        found=1; reclaim "$(basename "$site")/…/vendor" "$vd"
      done < <(find "$site" -maxdepth 6 -type d -name vendor -prune 2>/dev/null)
      (( found == 0 )) && kept "$(basename "$site") — dormant (${idle}d) but no deps"
    done
    info "restore any site later with: npm install / composer install"
  fi
fi

# ---------------------------------------------------------------------------------
# 10. Claude Desktop VM bundles (opt-in, risky — GB re-download)
# ---------------------------------------------------------------------------------
if [[ $DO_CLAUDE_VM -eq 1 ]]; then
  section "Claude Desktop VM bundles"
  if is_running 'claude-desktop|/opt/Claude'; then
    skip "Claude Desktop is running — close it before removing VM bundles"
  elif confirm "remove Claude VM bundles (re-downloads several GB on next use)?"; then
    reclaim "Claude vm_bundles" "$HOME/.config/Claude/vm_bundles"
  fi
fi

# ---------------------------------------------------------------------------------
# 10b. Claude Code background-job dirs (~/.claude/jobs) — opt-in
#      Each job dir holds state.json + timeline.jsonl + a tmp/ scratch space that
#      agents are told to use. That scratch regularly ends up holding whole
#      node_modules trees, so it is usually the single biggest thing in ~/.claude.
#
#      Three guards, all of which must pass before a job is touched:
#        1. it is not the job this script is running inside ($CLAUDE_JOB_DIR)
#        2. nothing in it was written in the last 60 minutes
#        3. its state is terminal (done/failed) — never running, never blocked
#           (blocked means a job is waiting on you and can still be resumed)
# ---------------------------------------------------------------------------------
if [[ $DO_CLAUDE_JOBS -eq 1 ]]; then
  section "Claude job dirs (finished; whole dir if > ${CLAUDE_JOBS_DAYS}d, else scratch only)"
  jobs_root="$CLAUDE_ROOT/jobs"
  if [[ ! -d "$jobs_root" ]]; then
    skip "no $jobs_root"
  else
    self_job=$(basename "${CLAUDE_JOB_DIR:-/nonexistent}")
    now_ts=$(date +%s)
    for jd in "$jobs_root"/*/; do
      jd=${jd%/}
      [[ -d "$jd" ]] || continue
      jid=$(basename "$jd")

      if [[ "$jid" == "$self_job" ]]; then
        kept "$jid — this script's own job"; continue
      fi
      # the daemon rewrites state.json/timeline.jsonl every turn, so a fresh mtime
      # on any direct child means the job is very likely still live
      if [[ -n "$(find "$jd" -maxdepth 1 -mmin -60 2>/dev/null)" ]]; then
        kept "$jid — written to within the hour"; continue
      fi

      jstate=none
      if [[ -f "$jd/state.json" ]]; then
        jstate=$(grep -oE '"state"[[:space:]]*:[[:space:]]*"[^"]+"' "$jd/state.json" |
                   head -1 | sed 's/.*"\([^"]*\)"$/\1/')
        jstate=${jstate:-unknown}
      fi
      case "$jstate" in
        done|failed) ;;                 # terminal — eligible
        none)        ;;                 # no state file: an orphaned stub, age-gated below
        *) kept "$jid — state=$jstate (not finished)"; continue ;;
      esac

      age_days=$(( (now_ts - $(stat -c %Y "$jd" 2>/dev/null || echo "$now_ts")) / 86400 ))
      if (( age_days >= CLAUDE_JOBS_DAYS )); then
        reclaim "job $jid ($jstate, ${age_days}d)" "$jd"
      elif [[ -d "$jd/tmp" ]]; then
        # too recent to drop the record, but tmp/ is documented throwaway scratch
        reclaim "job $jid scratch ($jstate, ${age_days}d)" "$jd/tmp"
      else
        kept "$jid — $jstate, ${age_days}d, no scratch"
      fi
    done
  fi
fi

# ---------------------------------------------------------------------------------
# 10c. Superseded Claude plugin versions (~/.claude/plugins/cache/<market>/<plugin>/)
#      Plugins are cached per version and old versions are never garbage-collected,
#      so a plugin that vendors node_modules leaves a full copy behind on every
#      upgrade. Keeps the newest version of each plugin, drops the rest.
# ---------------------------------------------------------------------------------
if [[ $DO_CLAUDE_PLUGINS -eq 1 ]]; then
  section "Superseded Claude plugin versions"
  plugin_cache="$CLAUDE_ROOT/plugins/cache"
  if [[ ! -d "$plugin_cache" ]]; then
    skip "no $plugin_cache"
  else
    info "an active session holding an old version keeps working until it restarts"
    for pdir in "$plugin_cache"/*/*/; do
      pdir=${pdir%/}
      [[ -d "$pdir" ]] || continue
      vers=()
      # strictly version-shaped names only — a "1.3.0.bak-pre-sync" dir must never
      # be mistaken for the newest release and win over the real 1.3.0
      mapfile -t vers < <(find "$pdir" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' 2>/dev/null |
                            grep -E '^v?[0-9]+(\.[0-9]+)+$' | sort -V)
      (( ${#vers[@]} > 1 )) || continue
      newest="${vers[${#vers[@]}-1]}"
      pname="$(basename "$(dirname "$pdir")")/$(basename "$pdir")"
      for v in "${vers[@]}"; do
        [[ "$v" == "$newest" ]] && continue
        reclaim "$pname $v" "$pdir/$v"
      done
      kept "$pname — keeping $newest"
    done
  fi
fi

# ---------------------------------------------------------------------------------
# 10d. Claude session history (opt-in, destructive to `claude --resume`)
#      Only top-level *.jsonl transcripts are removed. memory/ directories and
#      per-session subdirectories inside ~/.claude/projects are never touched.
# ---------------------------------------------------------------------------------
if [[ ${CLAUDE_HISTORY_DAYS:-0} -gt 0 ]]; then
  section "Claude session history (> ${CLAUDE_HISTORY_DAYS}d)"
  info "${C_YEL}⚠${C_RESET} removes 'claude --resume' for those sessions; memory/ is never touched"
  old_jsonl=()
  mapfile -t old_jsonl < <(find "$CLAUDE_ROOT/projects" -mindepth 2 -maxdepth 2 -type f \
                             -name '*.jsonl' -mtime "+$CLAUDE_HISTORY_DAYS" 2>/dev/null)
  if (( ${#old_jsonl[@]} )); then
    reclaim "session transcripts (${#old_jsonl[@]})" "${old_jsonl[@]}"
  else
    info "no transcripts older than ${CLAUDE_HISTORY_DAYS}d (Claude Code prunes these itself)"
  fi
  reclaim_stale "file-history"    "$CLAUDE_ROOT/file-history"    "$CLAUDE_HISTORY_DAYS"
  reclaim_stale "shell snapshots" "$CLAUDE_ROOT/shell-snapshots" "$CLAUDE_HISTORY_DAYS"
  reclaim_stale "session env"     "$CLAUDE_ROOT/session-env"     "$CLAUDE_HISTORY_DAYS"
  reclaim_stale "paste cache"     "$CLAUDE_ROOT/paste-cache"     "$CLAUDE_HISTORY_DAYS"
  kept "settings, memory/, plugins and todos preserved"
fi

# ---------------------------------------------------------------------------------
# 11. System-level (opt-in, sudo): apt, journal, old snap revisions
# ---------------------------------------------------------------------------------
if [[ $DO_SYSTEM -eq 1 ]]; then
  section "System (sudo)"
  if [[ $APPLY -eq 1 ]]; then
    sudo -v || { skip "no sudo — skipping system cleanup"; DO_SYSTEM=0; }
  fi
  if [[ $DO_SYSTEM -eq 1 ]]; then
    if [[ $APPLY -eq 1 ]]; then
      sudo apt-get clean         >/dev/null 2>&1 && info "apt cache cleaned"
      sudo apt-get autoremove --purge -y >/dev/null 2>&1 && info "orphan packages removed"
      sudo journalctl --vacuum-time=7d 2>&1 | tail -1 | sed 's/^/  /'
      # drop old snap revisions (keep current)
      if command -v snap >/dev/null 2>&1; then
        LANG=C snap list --all 2>/dev/null | awk '/disabled/{print $1, $3}' |
          while read -r sn rev; do sudo snap remove "$sn" --revision="$rev" >/dev/null 2>&1 && info "snap $sn r$rev removed"; done
      fi
    else
      info "would run: apt-get clean; apt-get autoremove --purge; journalctl --vacuum-time=7d; drop old snap revisions"
    fi
  fi
fi

# ---------------------------------------------------------------------------------
# summary
# ---------------------------------------------------------------------------------
after_avail=$(df -P "$HOME" | awk 'NR==2{print $4}')
delta_kb=$(( after_avail - before_avail ))
section "Summary"
if [[ $APPLY -eq 1 ]]; then
  printf '  %sFreed this run: %s%s\n' "$C_GRN$C_B" "$(human $((TOTAL_BYTES)))" "$C_RESET"
  printf '  Disk free on %s: %s → %s\n' "$HOME_FS" "$(human $((before_avail*1024)))" "$(human $((after_avail*1024)))"
  [[ $delta_kb -gt 0 ]] && printf '  %s(+%s available)%s\n' "$C_DIM" "$(human $((delta_kb*1024)))" "$C_RESET"
else
  printf '  %sReclaimable (dry-run): %s%s\n' "$C_CYN$C_B" "$(human $((TOTAL_BYTES)))" "$C_RESET"
  printf '  Re-run with %s--apply%s to reclaim it.\n' "$C_B" "$C_RESET"
fi
printf '  %sProtected & never touched: source trees, Downloads, ~/.ollama models, MEGA/Dropbox,\n  Docker named volumes, ~/.claude memory/settings, and any running Claude job.%s\n' "$C_DIM" "$C_RESET"
