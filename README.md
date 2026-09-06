<div align="center">

<img src="assets/icon-256.png" alt="reclaim icon" width="96" height="96">

# reclaim

**Reclaim disk space without losing anything you cannot get back.**

[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8.svg?logo=go&logoColor=white)](https://go.dev/)
[![Platform](https://img.shields.io/badge/Platform-Linux-FCC624.svg?logo=linux&logoColor=black)](#status)
[![Distros](https://img.shields.io/badge/distros-deb%20%7C%20rpm%20%7C%20arch%20%7C%20alpine-4C1.svg)](#install)
[![Dependencies](https://img.shields.io/badge/dependencies-none-4C1.svg)](go.mod)
[![Tests](https://img.shields.io/badge/tests-270-4C1.svg)](#development)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

</div>

## What it is

Every disk-cleanup tool faces the same question and most answer it badly: *what
is safe to delete?* Treat everything cache-shaped as disposable and you
eventually remove a browser's `Local Storage` and log someone out of everything,
or delete a dependency tree whose lockfile no longer resolves. Be too timid and
the tool is not worth running.

`reclaim` answers it by refusing to treat "regenerable" as one category. Every
target is a **unit** carrying a tier — from *costs nothing* to *may be
irreplaceable* — and a separate flag saying whether losing it destroys
information or merely costs a re-download. The planner walks tiers in order,
stops at a ceiling, and **an opt-in flag can raise that ceiling but can never
authorise destroying information**. Only `--allow-lossy` does that.

The second thing it takes seriously is that caches belong to programs that may
be running right now. Deleting a live IDE's cache corrupts the session it is in
the middle of. So before anything is selected, `reclaim` reads the process table
and parks every unit whose directories belong to something currently running,
then tells you what to quit and which pid to blame.

```console
$ reclaim clean
DRY-RUN — nothing was deleted. Re-run with --apply to clean.
...
== Locked by running apps ==
  • JetBrains caches    6.2GiB  held by jetbrains-ide (pid 1155531)
      quit it, then re-run
```

Nothing is deleted without `--apply`, and `--apply` prompts before it acts.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/mralaminahamed/reclaim/trunk/install.sh | sh
```

The script installs a **native package** where one fits the machine — `.deb` on
Debian and its derivatives, `.rpm` on Fedora, RHEL and openSUSE — so `reclaim`
can be upgraded and removed by the tools that already manage everything else
there. Anywhere else it falls back to a static binary in `/usr/local/bin`.
Checksums are verified, and a missing checksum file is a failure rather than a
shrug.

It selects on the *package format the machine expects*, not the distribution's
name. That is what makes it work on the derivatives too, and there are far more
of those than there are upstreams.

Or take a package straight from the
[releases](https://github.com/mralaminahamed/reclaim/releases):

```bash
sudo dpkg -i reclaim_0.2.0_amd64.deb            # Debian, Ubuntu, Mint, Pop!_OS
sudo dnf install reclaim-0.2.0-1.x86_64.rpm     # Fedora, RHEL, Rocky, Alma
sudo zypper install reclaim-0.2.0-1.x86_64.rpm  # openSUSE
```

Arch has a [PKGBUILD](packaging/PKGBUILD). With Go already installed,
`go install github.com/mralaminahamed/reclaim/cmd/reclaim@latest`. From a
checkout, `make build` — or `make dist deb rpm` to produce the packages
yourself.

There are no third-party dependencies. For a tool that deletes files as root, an
empty `require` block is a feature rather than an accident. The published
binaries are static for the same reason: this runs on a machine that is having a
bad day, and a dynamic link to a libc on the same failing disk is a bad bet.

## Usage

<div align="center">
<img src="assets/demo.png" alt="reclaim dry-run output" width="880">
</div>

```bash
reclaim clean                 # measure and report; deletes nothing
reclaim clean --apply         # reclaim, after confirming
reclaim status                # filesystems, free space, pressure
reclaim analyze --min 1G      # largest directories; advisory
reclaim analyze --installers  # stale downloads; advisory
reclaim history               # what past runs actually removed
reclaim completion bash       # shell completion script
```

### How hard to try

```bash
reclaim clean --free 12G    # clean until 12G is free, then stop
reclaim clean --auto        # read disk pressure and pick a target
reclaim clean --below 20G   # do nothing unless free space is under 20G
reclaim clean --tier 2      # never escalate past tier 2
```

`--auto` finds the most pressured filesystem and lets how full it is decide how
hard to try. A comfortable disk gets only the free tiers; a critical one earns a
cold reload.

### What to look at

```bash
reclaim clean --discover      # also claim caches with no hardcoded rule
reclaim clean --only 'xdg-*'  # restrict to matching unit ids
reclaim clean --exclude 'npm-*'
reclaim clean --workers 12    # size the probe pool
reclaim clean --json          # machine-readable output
```

### Opt-in groups

Units in these groups are **never** touched unless you name them. Irreversible
units additionally require `--allow-lossy`.

| Flag | Reclaims |
|---|---|
| `--system` | the package-manager cache, a bounded journal vacuum, old snap revisions, crash dumps |
| `--kernels` | superseded kernel packages, never the running one |
| `--models` | huggingface, torch, whisper and LM Studio model stores |
| `--flatpak` | unused runtimes, and each app's sandboxed cache |
| `--docker`, `--docker-volumes` | Docker prune — volumes may hold databases |
| `--gradle` | `~/.gradle/caches`, `~/.gradle/wrapper` |
| `--maven` | `~/.m2/repository` |
| `--jetbrains` | JetBrains IDE caches |
| `--browsers` | Chrome, Brave and Firefox HTTP caches |
| `--playwright` | Playwright browser binaries |
| `--claude-jobs`, `--claude-plugins` | Claude Code scratch and plugin cache |
| `--sites-idle N` | dependency and build trees of projects idle for N days |
| `--heavy` | discovered caches over 1GiB |

Set `RECLAIM_NO_OPLOG=1` to disable the operations log.

## What it knows about

### System packages

`--system` covers whichever package manager the machine has — `apt`, `dnf`,
`yum`, `pacman`, `zypper` or `apk` — chosen by which binary is present rather
than by parsing a distribution name. Each is a cache clean and nothing more.
None of them runs an autoremove: that decides for itself what is orphaned, and
the result cannot be previewed honestly.

It needs root, and `reclaim` asks for it once up front via `sudo -v` — a single
password prompt rather than one per unit. If elevation is declined the system
units are reported under **Failed** with the reason, never counted as reclaimed.

### Kernels

`--kernels` is deliberately separate from `--system`, and never runs
`apt autoremove`. Two kernels always survive — the running one and the newest —
and the dry run names every package it would purge, because a byte count is not
something anyone can consent to for a kernel removal.

Debian-only, and not because the others are harder: `dnf` enforces
`installonly_limit` itself, and Arch ships one `linux` package that is replaced
rather than accumulated. On those systems there is nothing to collect, and a
kernel unit would be inventing work.

### Model stores

Tier 3 rather than tier 1, because "comes back" and "comes back for free" are
different claims: every file re-downloads, over hours, often metered. Pruning is
separate and unflagged — `hf cache prune` discards only revisions nothing
references and downloads that never finished, so it costs nothing and leaves
working models alone.

### Crash artifacts

`/var/crash` and `/var/lib/systemd/coredump` are only taken once they are
**older than seven days**. That bound is what makes them safe to treat as
regenerable: a dump written this morning belongs to a crash someone may be
reading right now, and taking it would destroy the only copy of that failure.
One from last month is a record the system itself is configured to expire.

### Idle projects

`--sites-idle` knows around fifteen project shapes — Cargo, Maven, Gradle,
Python venvs, Next, Elixir, CocoaPods, Terraform, .NET, Zig, Dart and the
Node/PHP pair it started with. Each dependency directory is paired with a
manifest that proves what produced it, because `build`, `target`, `obj` and
`bin` are ordinary words and a directory of that name with nothing beside it is
somebody's source. Idleness is judged from a project's own files, never from its
build output.

### Stale installers

```bash
reclaim analyze --installers --older 90
```

Reports `.deb`, `.rpm`, `.AppImage`, `.iso` and friends in `~/Downloads` that
have sat there for months. This one **only ever reports**. Everything else the
tool touches lives in a cache directory, where the containing directory is
itself the argument that the contents are disposable; `~/Downloads` is the
opposite, and a download there may be the only copy of something.

Archives are not counted. A `.zip` is a container, not an intent, and on a real
`~/Downloads` the largest ones turned out to be a book collection and a Figma UI
kit.

Where redundancy can be proven rather than guessed — a `.deb` whose package is
already installed at exactly that version — the report says so:

```
  • ~/Downloads/coreutils.deb   20.2KiB  200d  coreutils 9.4-3ubuntu6.3 is already installed
```

## Configuration

### Defaults

`~/.config/reclaim/config.json` fills in flags that were not given:

```json
{
  "workers": 12,
  "tier": 3,
  "exclude": ["go-*", "cargo-*"],
  "sites_root": "~/Projects"
}
```

One rule holds the whole file together: **a config may narrow a run and may
never widen one.** So `apply`, `yes`, `allow_lossy`, `discover` and the opt-in
group names are refused, by name and with the reason, rather than accepted
quietly. A flag is typed at the moment of use and read back by whoever is about
to press enter; a file is not read back by anyone, and the person it would
surprise is the one who wrote it months ago and forgot. The worst a stale
exclusion can do is leave space unreclaimed, which the next report points out.

A file that exists and does not parse stops the run.

### Custom units

The catalog covers what this tool ships with an opinion about. Anything else — a
cache for a tool nobody here uses, a site-specific scratch directory — can be
declared in `~/.config/reclaim/units.json`, or dropped into
`/etc/reclaim/units.d/*.json` by a package:

```json
{
  "units": [
    {
      "id": "ccache",
      "label": "ccache objects",
      "tier": 1,
      "reversible": true,
      "paths": [".cache/ccache"]
    },
    {
      "id": "conda-pkgs",
      "label": "conda package cache",
      "tier": 1,
      "reversible": true,
      "command": "conda clean --all --yes",
      "requires": "conda"
    }
  ]
}
```

`tier` and `reversible` are required. They are the two claims the safety model
rests on, and defaulting either would let an omission make a promise the author
never made. Relative paths resolve under `$HOME`; `requires` drops the unit on
machines without that binary; `flag` makes it opt-in, named as `--with <name>`
since a file cannot register a CLI flag.

A file **adds** to the catalog and can never restate it — the shipped definition
of a unit id always wins. Definitions go through the same protected-path
backstop as everything else, so one aiming at `/`, a top-level system directory
or `$HOME` is refused at load. `reversible: false` still requires
`--allow-lossy`: writing a unit down does not lower that gate. A longer example
is in [docs/units.example.json](docs/units.example.json).

### Shell completion

```bash
reclaim completion bash > /etc/bash_completion.d/reclaim
reclaim completion zsh  > "${fpath[1]}/_reclaim"
reclaim completion fish > ~/.config/fish/completions/reclaim.fish
```

Completing `--only` and `--exclude` asks the binary for unit ids rather than
carrying a list, since which units exist depends on what is installed. The
packages install these for you.

### Running on a schedule

`--below` makes a run a no-op unless the disk is actually under pressure, and it
decides that before building a catalog or probing anything — so a timer can fire
often and cost nothing most of the time:

```bash
sudo cp systemd/reclaim.* /etc/systemd/system/
sudo systemctl enable --now reclaim.timer
```

The shipped service runs `clean --below 20G --auto --apply --yes`, with no
opt-in group flags and no `--allow-lossy`. Whatever it does, it does at 03:00
with nobody reading the output, and the default tier ceiling is the right amount
of ambition for that. `--json` and the operations log make a run auditable
afterwards.

There is no `watch` daemon on purpose. A timer plus `--below` is the same
capability without a process to supervise, and systemd already handles the parts
a daemon would have to reimplement badly: persistence across reboot, missed
windows, and logging.

## Safety model

- **Dry run by default.** Nothing is deleted and no command runs without
  `--apply`, which prompts unless given `--yes`.
- **Tiers.** Units run cheapest-first and the planner will not cross its ceiling.
- **Reversibility is absolute.** An opt-in flag authorises a unit but never
  authorises destroying information. A test pins that distinction.
- **Opt-in means opt-in.** A unit carrying a flag runs only when that flag is
  passed, whatever the tier ceiling says.
- **Locks.** Caches of running applications are skipped, named, and attributed
  to a pid. `reclaim` excludes its own process, so its command line cannot lock
  it out of its own work.
- **Probing never deletes.** Everything reachable from the measuring phase is
  read-only, asserted by a test that walks the tree before and after.
- **Protected names.** `Local Storage`, `IndexedDB`, `Cookies`, `Login Data` and
  friends are never treated as caches, in any casing.
- **Protected paths.** A backstop refuses `/`, top-level system directories and
  `$HOME` however a unit is defined, so a malformed unit cannot aim the deleter
  at the wrong tree.
- **The log says what went.** `reclaim history` records the paths each unit
  removed, not just which unit ran — after a `--discover` run the unit list was
  not knowable in advance. Long lists are trimmed with a true count, so a record
  never understates what happened. **Deletion is final: there is no undo.**

## How it works

```
survey → catalog → probe → lock → plan → run → report
```

The catalog registers a unit only when its path exists or its tool is installed.
Probing is `du`-bound and every unit is independent, so it runs on a worker
pool — a full discovery run over a developer machine takes about **1.7 seconds**.
Locking happens *after* probing, so a locked unit still reports its size and you
can see what quitting the app would buy you.

```
cmd/reclaim/         the CLI
internal/unit/       unit model and registry
internal/catalog/    the table of things worth reclaiming
internal/system/     package-manager caches, journal, kernels, crash artifacts
internal/scan/       dependency and build trees of idle projects
internal/probe/      parallel measurement
internal/lock/       running-application detection
internal/plan/       tier ceiling and selection
internal/runner/     execution, with a protected-path backstop
internal/discover/   caches with no hardcoded rule
internal/unitfile/   unit definitions loaded from files
internal/config/     defaults read from a file
internal/installers/ stale downloads, reported and never removed
internal/report/     text and JSON output
internal/oplog/      append-only record of what was deleted
internal/fsutil/     sizes, mounts, pressure and age
```

Discovery uses two different rules on purpose. Everything directly under
`~/.cache` is regenerable by the XDG basedir spec, so it is claimed by *location*
and needs no whitelist. A cache nested inside `~/.config` sits beside real
application state, so there it is claimed by *name* against a strict list.

Claiming by location is a sound argument that the contents are regenerable and
no argument at all about what regenerating them *costs*. A model cache and a
font cache are both disposable; only one of them is cheap. So once sizes are
known, any discovered unit over 1GiB is re-rated: it moves to the cold-reload
tier and becomes opt-in behind `--heavy`. It is still reported — with its size
and the flag that would claim it — because hiding the space would be no better
than deleting it unasked.

## Development

```bash
make test              # go test ./... and go vet ./...
go test ./... -race    # 270 tests across 16 packages
make dist deb rpm      # release artifacts
```

The CLI tests build the real binary and run it against a fixture `HOME`, so flag
parsing, exit codes, dry-run behaviour and JSON validity are covered end to end.
CI additionally runs the built binary on Debian, Fedora, Arch, openSUSE and
Alpine, because the claim that this works beyond Debian only stays true if
something keeps checking it somewhere that is not Debian.

Assets are generated, never hand-edited — editing them individually is what let
the palette drift the first time:

```bash
python3 assets/generate.py && bash assets/render.sh
```

## Status

Linux, on every distribution CI can reach: Debian, Ubuntu, Fedora, Arch,
openSUSE and Alpine. macOS support is planned and tracked in
[#1](https://github.com/mralaminahamed/reclaim/issues/1).

Note that `GOOS=darwin go build` currently *succeeds*, which is misleading: the
process table reader returns nothing on macOS, so every unit would look
unlocked. That is [#2](https://github.com/mralaminahamed/reclaim/issues/2) and
it blocks everything else — it is a safety regression, not a missing feature.

This began as a single self-contained bash script that reached 1615 lines and
201 built-in assertions. It is preserved in git history:

```bash
git show 67fd60c:reclaim.sh > reclaim.sh
```

## License

MIT — see [LICENSE](LICENSE).
