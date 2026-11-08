package organize

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const porcelain = `worktree C:/repo
HEAD aaaa1111
branch refs/heads/main

worktree C:/repo/.treehouse/1
HEAD bbbb2222
branch refs/heads/agent/one

worktree C:/repo/.herakles/tmp/wt-otomerge
HEAD cccc3333
branch refs/heads/ci/otomerge
locked agent-running

worktree C:/gone
HEAD dddd4444
detached
prunable gitdir file points to non-existent location
`

func TestParsePorcelain(t *testing.T) {
	entries := ParsePorcelain(porcelain)
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(entries))
	}
	if entries[1].Branch != "agent/one" || entries[1].Path == "" {
		t.Fatalf("branch parse: %+v", entries[1])
	}
	if !entries[2].Locked {
		t.Fatal("locked flag lost")
	}
	if !entries[3].Prunable || entries[3].Branch != "(detached)" {
		t.Fatalf("prunable/detached parse: %+v", entries[3])
	}
}

func TestClassify(t *testing.T) {
	pool := filepath.FromSlash("/repo/.treehouse")
	cases := []struct {
		name, path, main    string
		prunable            bool
		wantKind, wantOwner string
	}{
		{"main", filepath.FromSlash("/repo"), filepath.FromSlash("/repo"), false, "main", "disarida"},
		{"pool", filepath.FromSlash("/repo/.treehouse/1"), filepath.FromSlash("/repo"), false, "pool", "disarida"},
		{"orphan", filepath.FromSlash("/repo/.herakles/tmp/wt-otomerge"), filepath.FromSlash("/repo"), false, "orphan", "gecici"},
		{"stale", filepath.FromSlash("/gone"), filepath.FromSlash("/repo"), true, "stale", "disarida"},
	}
	for _, c := range cases {
		e := Entry{Path: c.path, Prunable: c.prunable}
		kind, owner := Classify(e, c.main, pool)
		if kind != c.wantKind || owner != c.wantOwner {
			t.Errorf("%s: got %s/%s want %s/%s", c.name, kind, owner, c.wantKind, c.wantOwner)
		}
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestListFindsOrphanWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git yok")
	}
	root := t.TempDir()
	main := filepath.Join(root, "repo")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, main, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(main, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, main, "add", ".")
	git(t, main, "commit", "-q", "-m", "ilk")
	orphan := filepath.Join(root, "agac", "wt-otomerge")
	git(t, main, "worktree", "add", "-q", "-b", "ci/test", orphan)

	trees, err := List(main, filepath.Join(main, ".treehouse"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var bulundu bool
	for _, w := range trees {
		if filepath.Clean(w.Path) == filepath.Clean(orphan) {
			bulundu = true
			if w.Kind != "orphan" || w.Branch != "ci/test" {
				t.Fatalf("orphan sinifi: %+v", w)
			}
			if w.Dirty != 0 || w.Ahead != 0 {
				t.Fatalf("temiz worktree: dirty=%d ahead=%d", w.Dirty, w.Ahead)
			}
		}
	}
	if !bulundu {
		t.Fatalf("orphan gorulmedi: %+v", trees)
	}
	if trees[0].Kind != "main" {
		t.Fatalf("ilk kayit main olmali: %+v", trees[0])
	}
}

func TestListAllAcrossRepos(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git yok")
	}
	root := t.TempDir()
	for _, ad := range []string{"bir", "iki"} {
		repo := filepath.Join(root, ad)
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, repo, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte(ad), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, repo, "add", ".")
		git(t, repo, "commit", "-q", "-m", "ilk")
		git(t, repo, "worktree", "add", "-q", "-b", "dal", filepath.Join(root, ad+"-wt"))
	}
	all, err := ListAll(root, func(string) string { return "" })
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("worktree = %d, want 4: %+v", len(all), all)
	}
	gorulen := map[string]bool{}
	for _, w := range all {
		if w.Repo == "" {
			t.Fatalf("repo etiketi bos: %+v", w)
		}
		gorulen[w.Repo] = true
	}
	if !gorulen["bir"] || !gorulen["iki"] {
		t.Fatalf("repo kapsami eksik: %v", gorulen)
	}
}
