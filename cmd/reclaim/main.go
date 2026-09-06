// Command reclaim reclaims disk space on Ubuntu and Debian machines.
//
// Safe by default: with no flags it measures and reports, and deletes nothing.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/mralaminahamed/reclaim/internal/catalog"
	"github.com/mralaminahamed/reclaim/internal/config"
	"github.com/mralaminahamed/reclaim/internal/discover"
	"github.com/mralaminahamed/reclaim/internal/fsutil"
	"github.com/mralaminahamed/reclaim/internal/installers"
	"github.com/mralaminahamed/reclaim/internal/lock"
	"github.com/mralaminahamed/reclaim/internal/oplog"
	"github.com/mralaminahamed/reclaim/internal/plan"
	"github.com/mralaminahamed/reclaim/internal/probe"
	"github.com/mralaminahamed/reclaim/internal/report"
	"github.com/mralaminahamed/reclaim/internal/runner"
	"github.com/mralaminahamed/reclaim/internal/scan"
	"github.com/mralaminahamed/reclaim/internal/system"
	"github.com/mralaminahamed/reclaim/internal/unit"
	"github.com/mralaminahamed/reclaim/internal/unitfile"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `reclaim — process-aware disk cleanup for Ubuntu and Debian

USAGE
  reclaim <command> [flags]

COMMANDS
  clean      report reclaimable space, or reclaim it with --apply
  status     show filesystems and disk pressure
  analyze    list the largest directories, deleting nothing
  history    show what past runs deleted
  version    print the version
  completion print a shell completion script

Run "reclaim <command> --help" for a command's flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}
	switch os.Args[1] {
	case "-h", "--help", "help":
		fmt.Print(usage)
	case "version", "--version":
		fmt.Println("reclaim", resolveVersion(version, moduleVersion()))
	case "clean":
		os.Exit(cmdClean(os.Args[2:]))
	case "status":
		os.Exit(cmdStatus(os.Args[2:]))
	case "analyze":
		os.Exit(cmdAnalyze(os.Args[2:]))
	case "history":
		os.Exit(cmdHistory(os.Args[2:]))
	case "completion":
		os.Exit(cmdCompletion(os.Args[2:]))
	case "__units":
		os.Exit(cmdUnits())
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// optInFlags are the boolean opt-in groups. Registration and forcing both read
// this one list, so a new group cannot be half-wired: defined but never
// honoured, or honoured under a name nothing defines.
var optInFlags = []string{"gradle", "maven", "jetbrains", "browsers", "playwright",
	"docker", "docker-volumes", "claude-vm", "system", "claude-jobs", "claude-plugins",
	"claude-history", "heavy", "flatpak", "kernels", "models"}

func cmdClean(args []string) int {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	var (
		apply      = fs.Bool("apply", false, "actually delete (default is a dry run)")
		yes        = fs.Bool("yes", false, "do not prompt before applying")
		free       = fs.String("free", "", "clean until SIZE is free, then stop (e.g. 12G)")
		auto       = fs.Bool("auto", false, "pick a target from current disk pressure")
		below      = fs.String("below", "", "do nothing unless free space is under SIZE")
		tier       = fs.Int("tier", int(unit.TierIrreplaceable), "highest tier to run without an opt-in flag")
		allowLossy = fs.Bool("allow-lossy", false, "permit units that destroy information")
		doDiscover = fs.Bool("discover", false, "also claim caches with no hardcoded rule")
		jsonOut    = fs.Bool("json", false, "machine-readable output")
		workers    = fs.Int("workers", runtime.NumCPU(), "parallel probe workers")
		sitesIdle  = fs.Int("sites-idle", 0, "include dependency trees of projects idle this many days")
		sitesRoot  = fs.String("sites-root", "", "where projects live (default ~/Sites)")
		only       multiFlag
		exclude    multiFlag
		flags      multiFlag
	)
	fs.Var(&only, "only", "restrict the run to these unit ids or globs (repeatable)")
	fs.Var(&exclude, "exclude", "drop these unit ids or globs (repeatable)")
	fs.Var(&flags, "with", "opt-in flag such as --gradle, passed as --with gradle (repeatable)")
	for _, name := range optInFlags {
		fs.Bool(name, false, "opt in to the "+name+" units")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	home, _ := os.UserHomeDir()

	// Config fills in only the flags that were not given. A file can narrow a
	// run and can never widen one, so precedence never has to arbitrate
	// anything dangerous: whatever the file says, the command line wins.
	cfg, err := config.Load(config.DefaultPath(home))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if cfg.Workers != nil && !given["workers"] {
		*workers = *cfg.Workers
	}
	if cfg.Tier != nil && !given["tier"] {
		*tier = *cfg.Tier
	}
	if cfg.JSON != nil && !given["json"] {
		*jsonOut = *cfg.JSON
	}
	if cfg.SitesRoot != "" && !given["sites-root"] {
		*sitesRoot = cfg.SitesRoot
	}
	if len(cfg.Only) > 0 && !given["only"] {
		only = cfg.Only
	}
	if len(cfg.Exclude) > 0 && !given["exclude"] {
		exclude = cfg.Exclude
	}

	forced := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		// sites-idle is not a bool, but giving it a value is the same act of
		// opting in, so it forces its units the same way.
		if f.Name == "sites-idle" || slices.Contains(optInFlags, f.Name) {
			forced["--"+f.Name] = true
		}
	})
	for _, f := range flags {
		forced["--"+strings.TrimPrefix(f, "--")] = true
	}

	// The threshold guard, before anything is built or measured. A timer that
	// fires hourly should almost always do nothing, and doing nothing should
	// cost nothing -- this is what makes a scheduled run cheap enough to
	// schedule often.
	if *below != "" {
		want, err := fsutil.ParseSize(*below)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		watched := home
		if *auto {
			if mounts, err := fsutil.Mounts(); err == nil && len(mounts) > 0 {
				watched = fsutil.Worst(mounts).Path
			}
		}
		// An unreadable filesystem is not evidence that there is room. The
		// guard only ever stops a run on a positive answer.
		avail, err := fsutil.AvailBytes(watched)
		if err == nil && avail >= want {
			fmt.Printf("%s has %s free, at or above the %s threshold — nothing to do\n",
				watched, fsutil.Human(avail), fsutil.Human(want))
			return 0
		}
	}

	// Build, measure, then lock. Locking after probing means a locked unit
	// still reports its size, so the user knows what quitting the app buys.
	env := catalog.DefaultEnv(home)
	reg := catalog.Build(env)
	// After the catalog, so the registry's first-wins rule means a file can add
	// to the shipped set but never restate it.
	if err := unitfile.Add(reg, home, unitfile.DefaultFiles(home), env.Has); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	// --kernels lives in the system package but is not part of --system: that
	// flag is documented as the apt cache, a bounded journal vacuum and old
	// snap revisions, and growing it to include package removal would change
	// what an existing command does.
	if forced["--system"] || forced["--kernels"] {
		system.Add(reg, system.DefaultEnv())
	}
	if *sitesIdle > 0 {
		root := *sitesRoot
		if root == "" {
			root = filepath.Join(home, "Sites")
		}
		scan.IdleProjects(reg, root, *sitesIdle)
	}
	if *doDiscover {
		discover.XDGCaches(reg, filepath.Join(home, ".cache"))
		discover.NestedCaches(reg, []string{
			filepath.Join(home, ".config"),
			filepath.Join(home, ".local", "share"),
		})
	}
	probe.All(reg, *workers)
	// A discovered unit was claimed for where it sits, so its tier is an
	// assumption about cost that nobody checked. Now that the size is known,
	// revisit it: a 2GiB cache is as regenerable as a 2MiB one and nothing
	// like as cheap.
	discover.PromoteHeavy(reg, discover.HeavyThreshold)
	lock.Apply(reg, lock.DefaultRules(home), lock.Running())

	opts := plan.Options{
		TierCap:    unit.Tier(*tier),
		AllowLossy: *allowLossy,
		Forced:     forced,
		Only:       only,
		Exclude:    exclude,
	}

	var targetBytes int64
	targetPath := home
	if *free != "" {
		n, err := fsutil.ParseSize(*free)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		targetBytes = n
		opts.TargetMount = fsutil.MountOf(home)
	} else if *auto {
		// Pressure-driven: aim the worst filesystem back under the high-water
		// mark, and let how bad it is decide how hard to try. A comfortable
		// disk gets only the free tiers; a critical one earns a cold reload.
		mounts, err := fsutil.Mounts()
		if err == nil && len(mounts) > 0 {
			worst := fsutil.Worst(mounts)
			opts.TargetMount = worst.Path
			targetPath = worst.Path

			const highWater = 85
			if worst.UsedPct > highWater {
				over := worst.Total * int64(worst.UsedPct-highWater) / 100
				targetBytes = worst.Avail + over
			}
			switch worst.Pressure() {
			case fsutil.PressureCritical:
				opts.TierCap = unit.TierColdReload
			case fsutil.PressureHigh:
				opts.TierCap = unit.TierArtifact
			case fsutil.PressureModerate:
				opts.TierCap = unit.TierPkgCache
			default:
				opts.TierCap = unit.TierNative
			}
			fmt.Printf("auto: %s is %d%% used (%s) — cleaning to tier %d\n",
				worst.Path, worst.UsedPct, worst.Pressure(), opts.TierCap)
		}
	}

	selected, withheld := plan.Select(reg, opts)
	// What Select passed over for want of a flag. Not running these is the
	// point of a flag; not mentioning them would just hide the space.
	optIn := plan.OptIn(reg, opts)
	var locked []*unit.Unit
	for _, u := range reg.All() {
		if u.LockedBy != "" {
			locked = append(locked, u)
		}
	}

	if *apply && !*yes && !confirm(selected) {
		fmt.Println("aborted")
		return 1
	}

	r := &runner.Runner{Apply: *apply, TargetBytes: targetBytes, TargetPath: targetPath}
	results := r.Run(selected)

	log := &oplog.Log{
		Path:     oplog.DefaultPath(home),
		Disabled: os.Getenv("RECLAIM_NO_OPLOG") != "",
	}
	var total int64
	var ran []*unit.Unit
	var failed []report.Failure
	for _, res := range results {
		if *apply {
			entry := oplog.Entry{At: time.Now(), UnitID: res.Unit.ID, Label: res.Unit.Label,
				Freed: res.Freed, Applied: true, Paths: res.Removed}
			if res.Err != nil {
				entry.Err = res.Err.Error()
			}
			_ = log.Append(entry)
		}
		// A unit that errored is not a unit that reclaimed anything. Counting it
		// under "Reclaimable" told the user space was freed when none was.
		if res.Err != nil {
			failed = append(failed, report.Failure{Unit: res.Unit, Reason: res.Err.Error()})
			continue
		}
		total += res.Freed
		ran = append(ran, res.Unit)
	}

	s := report.Summary{
		Selected: ran, Failed: failed, Locked: locked, Withheld: withheld, OptIn: optIn,
		TotalBytes: total, DryRun: !*apply, StoppedEarly: r.StoppedEarly,
	}
	if *jsonOut {
		if err := report.JSON(os.Stdout, s); err != nil {
			return 1
		}
		return 0
	}
	report.Text(os.Stdout, s)
	return 0
}

// confirmMessage is the last thing shown before anything is deleted, so it
// repeats whatever detail the report carried. Approving "1 location (700MiB)"
// is not the same as approving the removal of four named packages.
func confirmMessage(sel []*unit.Unit) string {
	var total int64
	var b strings.Builder
	for _, u := range sel {
		total += u.Bytes
	}
	for _, u := range sel {
		for _, d := range u.Detail {
			fmt.Fprintf(&b, "  %s: %s\n", u.Label, d)
		}
	}
	noun := "units"
	if len(sel) == 1 {
		noun = "unit"
	}
	fmt.Fprintf(&b, "About to run %d %s (%s). Continue? [y/N] ",
		len(sel), noun, fsutil.Human(total))
	return b.String()
}

func confirm(sel []*unit.Unit) bool {
	fmt.Print(confirmMessage(sel))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	mounts, err := fsutil.Mounts()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(mounts) == 0 {
		// Usually a container: the root filesystem is overlay, which is
		// excluded as a pseudo filesystem. Silence here reads as a broken
		// command rather than as an empty answer.
		fmt.Println("no real filesystems found (overlay and pseudo filesystems are not reported)")
		return 0
	}
	for _, m := range mounts {
		fmt.Printf("  %-28s %8s free of %8s  %3d%%  %s\n",
			m.Path, fsutil.Human(m.Avail), fsutil.Human(m.Total), m.UsedPct, m.Pressure())
	}
	return 0
}

func cmdAnalyze(args []string) int {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	min := fs.String("min", "500M", "only report directories at least this large")
	stale := fs.Bool("installers", false, "also report stale downloaded installers")
	older := fs.Int("older", 90, "how many days old an installer must be to be stale")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	n, err := fsutil.ParseSize(*min)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	home, _ := os.UserHomeDir()
	heavy := discover.Heavyweights([]string{home}, n)
	sum := report.Summary{Heavy: heavy, DryRun: true}
	if *stale {
		// ~/Downloads holds user files, not cache. This reports and never
		// removes, which is why it lives in analyze rather than as a unit.
		sum.Installers = installers.Find(installers.DefaultEnv(),
			filepath.Join(home, "Downloads"),
			time.Duration(*older)*24*time.Hour)
	}
	if *asJSON {
		if err := report.JSON(os.Stdout, sum); err != nil {
			return 1
		}
		return 0
	}
	report.Text(os.Stdout, sum)
	return 0
}

func cmdHistory(args []string) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	limit := fs.Int("n", 20, "how many entries to show")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	home, _ := os.UserHomeDir()
	entries, err := (&oplog.Log{Path: oplog.DefaultPath(home)}).Read(*limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(entries) == 0 {
		fmt.Println("no recorded runs yet")
		return 0
	}
	for _, e := range entries {
		fmt.Printf("  %s  %-28s %10s\n",
			e.At.Format("2006-01-02 15:04"), e.UnitID, fsutil.Human(e.Freed))
		for _, p := range e.Paths {
			fmt.Printf("      %s\n", p)
		}
		// The list is trimmed to keep a line per unit readable; say so rather
		// than let the entry look complete.
		if more := e.PathCount - len(e.Paths); more > 0 {
			fmt.Printf("      ... and %d more\n", more)
		}
	}
	return 0
}
