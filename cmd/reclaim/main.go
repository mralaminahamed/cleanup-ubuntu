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
	"strings"
	"time"

	"github.com/mralaminahamed/reclaim/internal/catalog"
	"github.com/mralaminahamed/reclaim/internal/discover"
	"github.com/mralaminahamed/reclaim/internal/fsutil"
	"github.com/mralaminahamed/reclaim/internal/lock"
	"github.com/mralaminahamed/reclaim/internal/oplog"
	"github.com/mralaminahamed/reclaim/internal/plan"
	"github.com/mralaminahamed/reclaim/internal/probe"
	"github.com/mralaminahamed/reclaim/internal/report"
	"github.com/mralaminahamed/reclaim/internal/runner"
	"github.com/mralaminahamed/reclaim/internal/scan"
	"github.com/mralaminahamed/reclaim/internal/system"
	"github.com/mralaminahamed/reclaim/internal/unit"
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
		fmt.Println("reclaim", version)
	case "clean":
		os.Exit(cmdClean(os.Args[2:]))
	case "status":
		os.Exit(cmdStatus(os.Args[2:]))
	case "analyze":
		os.Exit(cmdAnalyze(os.Args[2:]))
	case "history":
		os.Exit(cmdHistory(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func cmdClean(args []string) int {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	var (
		apply      = fs.Bool("apply", false, "actually delete (default is a dry run)")
		yes        = fs.Bool("yes", false, "do not prompt before applying")
		free       = fs.String("free", "", "clean until SIZE is free, then stop (e.g. 12G)")
		auto       = fs.Bool("auto", false, "pick a target from current disk pressure")
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
	for _, name := range []string{"gradle", "maven", "jetbrains", "browsers", "playwright",
		"docker", "docker-volumes", "claude-vm", "system", "claude-jobs", "claude-plugins",
		"claude-history"} {
		fs.Bool(name, false, "opt in to the "+name+" units")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	home, _ := os.UserHomeDir()
	forced := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "gradle", "maven", "jetbrains", "browsers", "playwright",
			"docker", "docker-volumes", "claude-vm", "system", "claude-jobs",
			"claude-plugins", "claude-history", "sites-idle":
			forced["--"+f.Name] = true
		}
	})
	for _, f := range flags {
		forced["--"+strings.TrimPrefix(f, "--")] = true
	}

	// Build, measure, then lock. Locking after probing means a locked unit
	// still reports its size, so the user knows what quitting the app buys.
	reg := catalog.Build(catalog.DefaultEnv(home))
	if forced["--system"] {
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
	for _, res := range results {
		total += res.Freed
		ran = append(ran, res.Unit)
		if *apply {
			entry := oplog.Entry{At: time.Now(), UnitID: res.Unit.ID, Label: res.Unit.Label,
				Freed: res.Freed, Applied: true}
			if res.Err != nil {
				entry.Err = res.Err.Error()
			}
			_ = log.Append(entry)
		}
		if res.Err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", res.Unit.ID, res.Err)
		}
	}

	s := report.Summary{
		Selected: ran, Locked: locked, Withheld: withheld,
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

func confirm(sel []*unit.Unit) bool {
	var total int64
	for _, u := range sel {
		total += u.Bytes
	}
	fmt.Printf("About to delete %d cache locations (%s). Continue? [y/N] ",
		len(sel), fsutil.Human(total))
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
	for _, m := range mounts {
		fmt.Printf("  %-28s %8s free of %8s  %3d%%  %s\n",
			m.Path, fsutil.Human(m.Avail), fsutil.Human(m.Total), m.UsedPct, m.Pressure())
	}
	return 0
}

func cmdAnalyze(args []string) int {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	min := fs.String("min", "500M", "only report directories at least this large")
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
	report.Text(os.Stdout, report.Summary{Heavy: heavy, DryRun: true})
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
	}
	return 0
}
