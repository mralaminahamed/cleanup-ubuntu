package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"1024", 1024, false},
		{"4K", 4 * 1024, false},
		{"2M", 2 * 1024 * 1024, false},
		{"12G", 12 * 1024 * 1024 * 1024, false},
		{"12g", 12 * 1024 * 1024 * 1024, false},
		{"1T", 1024 * 1024 * 1024 * 1024, false},
		{"10GB", 10 * 1024 * 1024 * 1024, false},
		// Leading zeros must stay decimal. The bash version fed these to shell
		// arithmetic, which read them as octal and aborted on 08 and 09.
		{"010G", 10 * 1024 * 1024 * 1024, false},
		{"09M", 9 * 1024 * 1024, false},
		{"08G", 8 * 1024 * 1024 * 1024, false},
		{"12.5G", 0, true},
		{"banana", 0, true},
		{"", 0, true},
		{"-5G", 0, true},
	}
	for _, c := range cases {
		got, err := ParseSize(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseSize(%q) = %d, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSize(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestHumanUsesBinaryUnits(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{999, "999B"},
		{1024, "1.0KiB"},
		{1536, "1.5KiB"},
		{2 * 1024 * 1024, "2.0MiB"},
		{3*1024*1024*1024 + 400*1024*1024, "3.4GiB"},
	}
	for _, c := range cases {
		if got := Human(c.in); got != c.want {
			t.Errorf("Human(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPathBytesSumsTree(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "a"), 1000)
	write(t, filepath.Join(sub, "b"), 2000)

	got, err := PathBytes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got < 3000 {
		t.Errorf("PathBytes = %d, want at least 3000", got)
	}
}

func TestPathBytesMissingPathIsZero(t *testing.T) {
	// A unit may list paths that do not exist on this machine. That is normal,
	// not an error: it contributes nothing and must not abort the probe.
	got, err := PathBytes(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 0 {
		t.Errorf("PathBytes(absent) = %d, want 0", got)
	}
}

func TestPathBytesDoesNotFollowSymlinks(t *testing.T) {
	// Following a symlink would double-count, or worse, walk out of the tree
	// being measured and into something we must never touch.
	dir := t.TempDir()
	target := t.TempDir()
	write(t, filepath.Join(target, "big"), 5000)
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unsupported here")
	}
	got, err := PathBytes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got >= 5000 {
		t.Errorf("PathBytes = %d, symlink target was followed", got)
	}
}

func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}
