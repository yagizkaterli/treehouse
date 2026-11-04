// Package organize inventories every git worktree of a repository — inside and
// outside the treehouse pool — so scattered agent worktrees stay visible.
package organize

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry is one record of `git worktree list --porcelain`.
type Entry struct {
	Path     string
	Head     string
	Branch   string
	Locked   bool
	Prunable bool
}

// Worktree is an Entry enriched with classification and repository facts.
type Worktree struct {
	Entry
	Repo       string
	Kind       string // "main" | "pool" | "orphan" | "stale"
	Owner      string
	Dirty      int
	Ahead      int
	Behind     int
	LastCommit time.Time
}

// ParsePorcelain parses `git worktree list --porcelain` output.
func ParsePorcelain(out string) []Entry {
	var entries []Entry
	var cur Entry
	flush := func() {
		if cur.Path != "" {
			entries = append(entries, cur)
		}
		cur = Entry{}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			cur.Path = val
		case "HEAD":
			cur.Head = val
		case "branch":
			cur.Branch = strings.TrimPrefix(val, "refs/heads/")
		case "detached":
			cur.Branch = "(detached)"
		case "locked":
			cur.Locked = true
		case "prunable":
			cur.Prunable = true
		}
	}
	flush()
	return entries
}

// Classify decides the kind and lane owner of a worktree path.
func Classify(e Entry, mainPath, poolDir string) (kind, owner string) {
	clean := filepath.Clean(e.Path)
	switch {
	case filepath.Clean(mainPath) == clean:
		kind = "main"
	case e.Prunable:
		kind = "stale"
	case poolDir != "" && within(clean, poolDir):
		kind = "pool"
	default:
		kind = "orphan"
	}
	return kind, laneOwner(clean)
}

func within(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// laneOwner derives a stable lane name from known treehouse roots.
func laneOwner(path string) string {
	p := filepath.ToSlash(path)
	candidates := []struct{ segment, owner string }{
		{"/.herakles/kovan/", "kovan"},
		{"/.herakles/tmp/", "gecici"},
		{"/.herakles/rust/worktrees/", "arsiv"},
		{"/.herakles/worktrees/", "havuz"},
	}
	for _, c := range candidates {
		if strings.Contains(p+"/", c.segment) {
			return c.owner
		}
	}
	return "disarida"
}

// List returns every worktree of repoRoot, classified and enriched.
func List(repoRoot, poolDir string) ([]Worktree, error) {
	out, err := run(repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	entries := ParsePorcelain(out)
	if len(entries) == 0 {
		return nil, nil
	}
	mainPath := entries[0].Path
	mainRef := mainRefOf(repoRoot, entries[0])
	trees := make([]Worktree, 0, len(entries))
	for _, e := range entries {
		kind, owner := Classify(e, mainPath, poolDir)
		wt := Worktree{Entry: e, Kind: kind, Owner: owner}
		wt.Dirty = dirtyCount(e.Path)
		if !e.Prunable {
			wt.Ahead, wt.Behind = aheadBehind(e.Path, mainRef)
			wt.LastCommit = lastCommit(e.Path)
		}
		trees = append(trees, wt)
	}
	sort.SliceStable(trees, func(i, j int) bool {
		if trees[i].Kind != trees[j].Kind {
			return trees[i].Kind < trees[j].Kind
		}
		return trees[i].Path < trees[j].Path
	})
	return trees, nil
}

// ListAll inventories every immediate child directory of root that is a git
// repository. poolFor resolves the pool directory of one repository, or "".
func ListAll(root string, poolFor func(repoRoot string) string) ([]Worktree, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var all []Worktree
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		repo := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
			continue
		}
		trees, err := List(repo, poolFor(repo))
		if err != nil {
			continue
		}
		for i := range trees {
			trees[i].Repo = e.Name()
		}
		all = append(all, trees...)
	}
	return all, nil
}

func mainRefOf(repoRoot string, main Entry) string {
	if main.Branch != "" && main.Branch != "(detached)" {
		return main.Branch
	}
	if ref, err := run(repoRoot, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		return strings.TrimSpace(ref)
	}
	return main.Head
}

func dirtyCount(path string) int {
	out, err := run(path, "status", "--porcelain")
	if err != nil {
		return -1
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return 0
	}
	return strings.Count(out, "\n") + 1
}

func aheadBehind(path, ref string) (int, int) {
	out, err := run(path, "rev-list", "--left-right", "--count", ref+"...HEAD")
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0
	}
	behind, _ := strconv.Atoi(fields[0])
	ahead, _ := strconv.Atoi(fields[1])
	return ahead, behind
}

func lastCommit(path string) time.Time {
	out, err := run(path, "log", "-1", "--format=%ct")
	if err != nil {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// Prune drops metadata for missing worktree directories (safe: no files touched).
func Prune(repoRoot string) (string, error) {
	return run(repoRoot, "worktree", "prune")
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	err := cmd.Run()
	return out.String(), err
}
