package project

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"wfm-rag/internal/cards"
	"wfm-rag/internal/index"
)

// testState isolates HOME so the state directory resolves inside the temp dir.
// XDG_DATA_HOME stays unset; a dedicated test covers the XDG rule.
func testState(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	return filepath.Join(home, ".local", "share", "wfm-rag")
}

func makeRepo(t *testing.T, name string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "knowledge", "patterns"), 0o755); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	card := "---\nid: card-" + name + "\ntype: pattern\nstatus: active\n---\n\n# Card " + name + "\n\nBody text.\n"
	if err := os.WriteFile(filepath.Join(repo, ".agents", "knowledge", "patterns", "card.md"), []byte(card), 0o644); err != nil {
		t.Fatalf("write card: %v", err)
	}
	return repo
}

func registryRaw(t *testing.T) string {
	t.Helper()
	path, err := RegistryPath()
	if err != nil {
		t.Fatalf("RegistryPath() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	return string(data)
}

func writeRawRegistry(t *testing.T, stateDir string, raw string) {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, registryName), []byte(raw), 0o600); err != nil {
		t.Fatalf("write raw registry: %v", err)
	}
}

func TestStateDirResolution(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(t.TempDir(), "xdg")

	cases := []struct {
		name    string
		home    string
		xdg     string
		wantDir string
	}{
		{name: "absolute XDG wins", home: home, xdg: xdg, wantDir: filepath.Join(xdg, "wfm-rag")},
		{name: "relative XDG falls back", home: home, xdg: "relative/path", wantDir: filepath.Join(home, ".local", "share", "wfm-rag")},
		{name: "empty XDG falls back", home: home, xdg: "", wantDir: filepath.Join(home, ".local", "share", "wfm-rag")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HOME", test.home)
			t.Setenv("XDG_DATA_HOME", test.xdg)
			path, err := RegistryPath()
			if err != nil {
				t.Fatalf("RegistryPath() error = %v", err)
			}
			want := filepath.Join(test.wantDir, registryName)
			if path != want {
				t.Errorf("RegistryPath() = %q, want %q", path, want)
			}
		})
	}

	t.Run("empty HOME errors", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("XDG_DATA_HOME", "")
		if _, err := RegistryPath(); err == nil {
			t.Error("RegistryPath() error = nil, want an error for an empty HOME")
		}
	})
}

func TestInitCreatesRegistryWithModes(t *testing.T) {
	state := testState(t)
	repo := makeRepo(t, "api")

	result, err := Init("api", repo)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !result.Created || result.Rebound || result.PreviousRoot != "" {
		t.Errorf("Init() = %+v, want a fresh registration", result)
	}

	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	entry, ok := registry.Projects["api"]
	if !ok || entry.Root != result.Root {
		t.Fatalf("registry = %+v, want api registered", registry)
	}

	info, err := os.Lstat(filepath.Join(state, registryName))
	if err != nil {
		t.Fatalf("stat registry: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("registry mode = %v, want 0600", info.Mode().Perm())
	}
	info, err = os.Lstat(state)
	if err != nil {
		t.Fatalf("stat state directory: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("state directory mode = %v, want 0700", info.Mode().Perm())
	}
	info, err = os.Lstat(filepath.Join(state, lockName))
	if err != nil {
		t.Fatalf("stat lock file: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		t.Errorf("lock file mode = %v, want a 0600 regular file", info.Mode().Perm())
	}
}

func TestInitRebindAndRefresh(t *testing.T) {
	testState(t)
	first := makeRepo(t, "first")
	second := makeRepo(t, "second")

	if _, err := Init("api", first); err != nil {
		t.Fatalf("Init(first) error = %v", err)
	}
	result, err := Init("api", second)
	if err != nil {
		t.Fatalf("Init(second) error = %v", err)
	}
	if !result.Rebound || result.PreviousRoot == "" || result.Created {
		t.Errorf("Init(second) = %+v, want a rebind reporting the previous root", result)
	}
	canonicalSecond, err := cards.CanonicalRoot(second)
	if err != nil {
		t.Fatalf("CanonicalRoot() error = %v", err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if registry.Projects["api"].Root != canonicalSecond {
		t.Errorf("registered root = %q, want %q", registry.Projects["api"].Root, canonicalSecond)
	}

	result, err = Init("api", second)
	if err != nil {
		t.Fatalf("Init(refresh) error = %v", err)
	}
	if result.Created || result.Rebound || result.PreviousRoot != "" {
		t.Errorf("Init(refresh) = %+v, want a no-op refresh", result)
	}
}

func TestInitCanonicalizesSymlinkedDirectory(t *testing.T) {
	testState(t)
	real := makeRepo(t, "real")
	link := filepath.Join(filepath.Dir(real), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := Init("api", link); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	canonical, err := cards.CanonicalRoot(real)
	if err != nil {
		t.Fatalf("CanonicalRoot() error = %v", err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if registry.Projects["api"].Root != canonical {
		t.Errorf("registered root = %q, want the canonical %q", registry.Projects["api"].Root, canonical)
	}
}

func TestInitAndRemoveIDValidation(t *testing.T) {
	testState(t)
	repo := makeRepo(t, "repo")

	valid := []string{"a", "api", "api-2", "9", "0", strings.Repeat("a", 32)}
	for _, id := range valid {
		if _, err := Init(id, repo); err != nil {
			t.Errorf("Init(%q) error = %v, want acceptance", id, err)
		}
	}

	invalid := []string{"", " ", "Api", "API", "-api", "api_", "api id", strings.Repeat("a", 33), "api..id", "ünapi"}
	for _, id := range invalid {
		if _, err := Init(id, repo); !errors.Is(err, ErrIDInvalid) {
			t.Errorf("Init(%q) error = %v, want ErrIDInvalid", id, err)
		}
		if _, err := Remove(id); !errors.Is(err, ErrIDInvalid) {
			t.Errorf("Remove(%q) error = %v, want ErrIDInvalid", id, err)
		}
	}
}

func TestInitMissingDirectoryErrors(t *testing.T) {
	testState(t)
	if _, err := Init("api", filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("Init() error = nil, want an error for a missing directory")
	}
}

func TestInitLimitExceeded(t *testing.T) {
	testState(t)
	base := t.TempDir()
	for i := 0; i < MaxProjects; i++ {
		id := fmt.Sprintf("p%02d", i)
		repo := filepath.Join(base, id)
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatalf("create repo: %v", err)
		}
		if _, err := Init(id, repo); err != nil {
			t.Fatalf("Init(%q) error = %v", id, err)
		}
	}
	if _, err := Init("overflow", base); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("Init(overflow) error = %v, want ErrLimitExceeded", err)
	}
	if _, err := Init("p00", filepath.Join(base, "p00")); err != nil {
		t.Errorf("re-Init of an existing ID at a full registry error = %v, want acceptance", err)
	}
}

func TestRemove(t *testing.T) {
	testState(t)
	repo := makeRepo(t, "repo")
	if _, err := Init("api", repo); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	result, err := Remove("api")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if result.ID != "api" || result.Root == "" {
		t.Errorf("Remove() = %+v, want the removed entry reported", result)
	}
	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(registry.Projects) != 0 {
		t.Errorf("registry = %+v, want it empty", registry)
	}
	if _, err := Remove("api"); !errors.Is(err, ErrUnknownID) {
		t.Errorf("Remove(api) error = %v, want ErrUnknownID", err)
	}
}

func TestLoadMissing(t *testing.T) {
	testState(t)
	if _, err := Load(); !errors.Is(err, ErrRegistryMissing) {
		t.Errorf("Load() error = %v, want ErrRegistryMissing", err)
	}
}

func TestLoadRejectsMalformedRegistry(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "trailing data", raw: `{"version":1,"projects":{"api":{"root":"/a"}}} {"version":1,"projects":{}}`},
		{name: "duplicate top-level key", raw: `{"version":1,"version":1,"projects":{}}`},
		{name: "duplicate project key", raw: `{"version":1,"projects":{"api":{"root":"/a"},"api":{"root":"/b"}}}`},
		{name: "duplicate entry key", raw: `{"version":1,"projects":{"api":{"root":"/a","root":"/b"}}}`},
		{name: "unknown top-level field", raw: `{"version":1,"projects":{},"extra":1}`},
		{name: "unknown entry field", raw: `{"version":1,"projects":{"api":{"root":"/a","extra":true}}}`},
		{name: "missing version", raw: `{"projects":{}}`},
		{name: "missing projects", raw: `{"version":1}`},
		{name: "unsupported version", raw: `{"version":2,"projects":{}}`},
		{name: "non-numeric version", raw: `{"version":"1","projects":{}}`},
		{name: "missing root", raw: `{"version":1,"projects":{"api":{}}}`},
		{name: "non-string root", raw: `{"version":1,"projects":{"api":{"root":42}}}`},
		{name: "empty root", raw: `{"version":1,"projects":{"api":{"root":""}}}`},
		{name: "relative root", raw: `{"version":1,"projects":{"api":{"root":"Services/Api"}}}`},
		{name: "malformed project ID", raw: `{"version":1,"projects":{"Bad":{"root":"/a"}}}`},
		{name: "not JSON", raw: `projects.json`},
		{name: "array instead of object", raw: `[]`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state := testState(t)
			writeRawRegistry(t, state, test.raw)
			if _, err := Load(); !errors.Is(err, ErrRegistryInvalid) {
				t.Errorf("Load() error = %v, want ErrRegistryInvalid for %s", err, test.raw)
			}
		})
	}
}

func TestLoadRejectsOversizedRegistry(t *testing.T) {
	state := testState(t)
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(state, registryName),
		[]byte(strings.Repeat(" ", MaxRegistryBytes+1)), 0o600); err != nil {
		t.Fatalf("write oversized registry: %v", err)
	}
	if _, err := Load(); !errors.Is(err, ErrRegistryInvalid) {
		t.Errorf("Load() error = %v, want ErrRegistryInvalid", err)
	}
}

func TestLoadRejectsUnsafeRegistryTypes(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		state := testState(t)
		if _, err := Init("api", makeRepo(t, "repo")); err != nil {
			t.Fatalf("Init() error = %v", err)
		}
		path := filepath.Join(state, registryName)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read registry: %v", err)
		}
		real := filepath.Join(state, "real-registry.json")
		if err := os.WriteFile(real, data, 0o600); err != nil {
			t.Fatalf("write real registry: %v", err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove registry: %v", err)
		}
		if err := os.Symlink(real, path); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if _, err := Load(); !errors.Is(err, ErrRegistryInvalid) {
			t.Errorf("Load() error = %v, want ErrRegistryInvalid for a symlink", err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		state := testState(t)
		if err := os.MkdirAll(filepath.Join(state, registryName), 0o700); err != nil {
			t.Fatalf("create directory at registry path: %v", err)
		}
		if _, err := Load(); !errors.Is(err, ErrRegistryInvalid) {
			t.Errorf("Load() error = %v, want ErrRegistryInvalid for a directory", err)
		}
	})
}

func TestRenderingIsSortedByProjectID(t *testing.T) {
	testState(t)
	repo := makeRepo(t, "repo")
	for _, id := range []string{"zulu", "mike", "alpha"} {
		if _, err := Init(id, repo); err != nil {
			t.Fatalf("Init(%q) error = %v", id, err)
		}
	}
	raw := registryRaw(t)
	first := strings.Index(raw, `"alpha"`)
	second := strings.Index(raw, `"mike"`)
	third := strings.Index(raw, `"zulu"`)
	if first < 0 || second < first || third < second {
		t.Errorf("registry rendering is not sorted by ID:\n%s", raw)
	}
}

func TestFailedWritePreservesPreviousRegistry(t *testing.T) {
	state := testState(t)
	repo := makeRepo(t, "repo")
	if _, err := Init("api", repo); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := os.Chmod(state, 0o500); err != nil {
		t.Fatalf("chmod state directory: %v", err)
	}
	if _, err := Init("extra", repo); err == nil {
		t.Fatal("Init() error = nil, want a failure with a read-only state directory")
	}
	if err := os.Chmod(state, 0o700); err != nil {
		t.Fatalf("restore state directory: %v", err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := registry.Projects["api"]; !ok {
		t.Errorf("registry = %+v, want the previous registry preserved", registry)
	}
	if _, ok := registry.Projects["extra"]; ok {
		t.Errorf("registry = %+v, want the failed entry absent", registry)
	}
}

func TestFlockBlocksSecondWriter(t *testing.T) {
	state := testState(t)
	repo := makeRepo(t, "repo")
	lockPath := filepath.Join(state, lockName)
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
		holder.Close()
		t.Fatalf("flock: %v", err)
	}
	if _, err := Init("api", repo); !errors.Is(err, ErrLocked) {
		t.Errorf("Init() error = %v, want ErrLocked while the lock is held", err)
	}
	if _, err := Remove("api"); !errors.Is(err, ErrLocked) {
		t.Errorf("Remove() error = %v, want ErrLocked while the lock is held", err)
	}
	syscall.Flock(int(holder.Fd()), syscall.LOCK_UN)
	holder.Close()
	if _, err := Init("api", repo); err != nil {
		t.Errorf("Init() error = %v, want success after the lock is released", err)
	}
}

func TestKilledWriterLeavesNoStaleLock(t *testing.T) {
	if os.Getenv("WFM_RAG_LOCK_HELPER") != "" {
		dir, err := stateDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "helper state directory: %v\n", err)
			os.Exit(1)
		}
		holder, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "helper open:", err)
			os.Exit(1)
		}
		if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
			fmt.Fprintln(os.Stderr, "helper flock:", err)
			os.Exit(1)
		}
		fmt.Println("locked")
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}

	testState(t)
	if _, err := Init("seed", makeRepo(t, "seed")); err != nil {
		t.Fatalf("seed Init() error = %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestKilledWriterLeavesNoStaleLock", "-test.count=1")
	cmd.Env = append(os.Environ(), "WFM_RAG_LOCK_HELPER=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	line := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			line <- scanner.Text()
			return
		}
		line <- ""
	}()
	var first string
	select {
	case first = <-line:
	case <-time.After(20 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("helper did not report the lock within 20 seconds")
	}
	if first != "locked" {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("helper output = %q, want %q", first, "locked")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	cmd.Wait()

	if _, err := Init("api", makeRepo(t, "repo")); err != nil {
		t.Errorf("Init() error = %v, want success after the lock holder was killed", err)
	}
}

func TestConcurrentInitRemoveKeepBothUpdates(t *testing.T) {
	testState(t)
	repo := makeRepo(t, "repo")
	for _, id := range []string{"alpha", "beta"} {
		if _, err := Init(id, repo); err != nil {
			t.Fatalf("Init(%q) error = %v", id, err)
		}
	}

	retryWrite := func(run func() error) error {
		var last error
		for attempt := 0; attempt < 100; attempt++ {
			last = run()
			if last == nil || !errors.Is(last, ErrLocked) {
				return last
			}
			time.Sleep(10 * time.Millisecond)
		}
		return last
	}

	var group sync.WaitGroup
	var initErr, removeErr error
	group.Add(2)
	go func() {
		defer group.Done()
		initErr = retryWrite(func() error {
			_, err := Init("gamma", repo)
			return err
		})
	}()
	go func() {
		defer group.Done()
		removeErr = retryWrite(func() error {
			_, err := Remove("alpha")
			return err
		})
	}()
	group.Wait()
	if initErr != nil {
		t.Fatalf("concurrent Init() error = %v", initErr)
	}
	if removeErr != nil {
		t.Fatalf("concurrent Remove() error = %v", removeErr)
	}

	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := registry.Projects["gamma"]; !ok {
		t.Errorf("registry = %+v, want gamma registered", registry)
	}
	if _, ok := registry.Projects["alpha"]; ok {
		t.Errorf("registry = %+v, want alpha removed", registry)
	}
	if _, ok := registry.Projects["beta"]; !ok {
		t.Errorf("registry = %+v, want beta untouched", registry)
	}
}

func TestStatuses(t *testing.T) {
	state := testState(t)
	alive := makeRepo(t, "alive")
	if _, err := Init("alive", alive); err != nil {
		t.Fatalf("Init(alive) error = %v", err)
	}
	if _, err := index.Rebuild(alive, ""); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	ghost := makeRepo(t, "ghost")
	if _, err := Init("ghost", ghost); err != nil {
		t.Fatalf("Init(ghost) error = %v", err)
	}
	if err := os.RemoveAll(ghost); err != nil {
		t.Fatalf("remove ghost repo: %v", err)
	}

	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	statuses, err := Statuses(registry)
	if err != nil {
		t.Fatalf("Statuses() error = %v", err)
	}
	if len(statuses) != 2 || statuses[0].ID != "alive" || statuses[1].ID != "ghost" {
		t.Fatalf("statuses = %+v, want both entries sorted by ID", statuses)
	}
	if statuses[0].RootMissing || statuses[0].IndexMissing {
		t.Errorf("alive status = %+v, want a live root and a present index", statuses[0])
	}
	if !statuses[1].RootMissing || !statuses[1].IndexMissing {
		t.Errorf("ghost status = %+v, want root missing and index missing", statuses[1])
	}

	t.Run("non-regular index file counts as missing", func(t *testing.T) {
		if err := os.Remove(statuses[0].IndexPath); err != nil {
			t.Fatalf("remove index: %v", err)
		}
		if err := os.MkdirAll(statuses[0].IndexPath, 0o700); err != nil {
			t.Fatalf("create directory at index path: %v", err)
		}
		statuses, err := Statuses(registry)
		if err != nil {
			t.Fatalf("Statuses() error = %v", err)
		}
		if !statuses[0].IndexMissing {
			t.Errorf("alive status = %+v, want index missing for a directory at the index path", statuses[0])
		}
	})
	_ = state
}

func TestHealthcheck(t *testing.T) {
	state := testState(t)
	alive := makeRepo(t, "alive")
	if _, err := Init("alive", alive); err != nil {
		t.Fatalf("Init(alive) error = %v", err)
	}
	build, err := index.Rebuild(alive, "")
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	dead := makeRepo(t, "dead")
	if _, err := Init("dead", dead); err != nil {
		t.Fatalf("Init(dead) error = %v", err)
	}
	if err := os.RemoveAll(dead); err != nil {
		t.Fatalf("remove dead repo: %v", err)
	}
	dangling := filepath.Join(state, "dangling")
	if err := os.MkdirAll(dangling, 0o700); err != nil {
		t.Fatalf("create dangling directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dangling, "index.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write dangling index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(state, "stray.txt"), []byte("stray\n"), 0o600); err != nil {
		t.Fatalf("write stray file: %v", err)
	}

	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	findings, err := Healthcheck(registry, false)
	if err != nil {
		t.Fatalf("Healthcheck() error = %v", err)
	}
	deadRoot := registry.Projects["dead"].Root
	deadIndex, err := index.DefaultPathFromCanonical(deadRoot)
	if err != nil {
		t.Fatalf("DefaultPathFromCanonical() error = %v", err)
	}
	want := []Finding{
		{Kind: "index_dangling", Path: dangling, Detail: "index directory is not claimed by any registered project"},
		{Kind: "index_missing", Project: "dead", Path: deadIndex, Detail: "no regular index file at the derived path"},
		{Kind: "root_missing", Project: "dead", Path: deadRoot, Detail: "registered root is not an existing directory"},
		{Kind: "state_unexpected", Path: filepath.Join(state, "stray.txt"), Detail: "unexpected regular file in state directory"},
	}
	if len(findings) != len(want) {
		t.Fatalf("findings = %+v, want %d findings", findings, len(want))
	}
	for position := range want {
		got, expected := findings[position], want[position]
		if got.Kind != expected.Kind || got.Project != expected.Project || got.Path != expected.Path {
			t.Errorf("finding %d = %+v, want %+v", position, got, expected)
		}
	}

	t.Run("verify reports a corrupt claimed index", func(t *testing.T) {
		if err := os.WriteFile(build.Path, []byte("not an index\n"), 0o600); err != nil {
			t.Fatalf("corrupt index: %v", err)
		}
		findings, err := Healthcheck(registry, true)
		if err != nil {
			t.Fatalf("Healthcheck() error = %v", err)
		}
		corrupt := false
		for _, finding := range findings {
			if finding.Kind == "index_invalid" && finding.Project == "alive" && finding.Path == build.Path {
				corrupt = true
			}
		}
		if !corrupt {
			t.Errorf("findings = %+v, want an index_invalid finding for the corrupt index", findings)
		}
	})
}

func TestHealthcheckHealthyIsEmpty(t *testing.T) {
	testState(t)
	repo := makeRepo(t, "repo")
	if _, err := Init("api", repo); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if _, err := index.Rebuild(repo, ""); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	findings, err := Healthcheck(registry, true)
	if err != nil {
		t.Fatalf("Healthcheck() error = %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %+v, want none for a healthy state", findings)
	}
}

func TestPurgeDanglingOnlySafeDirectories(t *testing.T) {
	state := testState(t)
	repo := makeRepo(t, "repo")
	if _, err := Init("api", repo); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	build, err := index.Rebuild(repo, "")
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	safe := filepath.Join(state, "safe-dangling")
	unsafe := filepath.Join(state, "unsafe-dangling")
	empty := filepath.Join(state, "empty-dangling")
	for _, dir := range []string{safe, unsafe, empty} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create dangling directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write dangling index: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(unsafe, "extra.txt"), []byte("extra\n"), 0o600); err != nil {
		t.Fatalf("write extra file: %v", err)
	}
	if err := os.Remove(filepath.Join(empty, "index.json")); err != nil {
		t.Fatalf("remove empty index: %v", err)
	}

	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	purged, retained, err := PurgeDangling(registry)
	if err != nil {
		t.Fatalf("PurgeDangling() error = %v", err)
	}
	if len(purged) != 1 || purged[0] != safe {
		t.Errorf("purged = %v, want only %q", purged, safe)
	}
	if len(retained) != 2 {
		t.Errorf("retained = %+v, want the unsafe and empty directories reported", retained)
	}
	for _, dir := range []string{unsafe, empty} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("retained directory %q was removed: %v", dir, err)
		}
	}
	if _, err := os.Stat(safe); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("purged directory still exists: %v", err)
	}
	if _, err := os.Stat(build.Path); err != nil {
		t.Errorf("claimed index was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, registryName)); err != nil {
		t.Errorf("registry was disturbed by purge: %v", err)
	}
}

func TestProjectPaths(t *testing.T) {
	testState(t)
	repo := makeRepo(t, "repo")
	for _, id := range []string{"zulu", "alpha"} {
		if _, err := Init(id, repo); err != nil {
			t.Fatalf("Init(%q) error = %v", id, err)
		}
	}
	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	paths, err := ProjectPaths(registry)
	if err != nil {
		t.Fatalf("ProjectPaths() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("ProjectPaths() = %v, want one path for the shared root", paths)
	}
	want, err := index.DefaultPath(registry.Projects["alpha"].Root)
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	if paths[0] != want {
		t.Errorf("ProjectPaths() = %v, want %v", paths, want)
	}
}

func TestInitAndRemoveRepairVanishedRoot(t *testing.T) {
	testState(t)
	repo := makeRepo(t, "repo")
	if _, err := Init("api", repo); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatalf("remove repo: %v", err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want a structurally valid registry after the root vanished", err)
	}
	if _, err := Statuses(registry); err != nil {
		t.Fatalf("Statuses() error = %v after a vanished root", err)
	}
	if _, err := Remove("api"); err != nil {
		t.Errorf("Remove() error = %v, want repair to succeed after a vanished root", err)
	}
	replacement := makeRepo(t, "replacement")
	if _, err := Init("api", replacement); err != nil {
		t.Errorf("Init() error = %v, want re-registration to succeed after a vanished root", err)
	}
}
