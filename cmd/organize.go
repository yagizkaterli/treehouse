package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/kunchenguid/treehouse/internal/config"
	"github.com/kunchenguid/treehouse/internal/organize"
	"github.com/kunchenguid/treehouse/internal/vcs"
)

var (
	organizeJSON  bool
	organizePrune bool
	organizeRepo  string
	organizePR    bool
	organizeAll   bool
	organizeKok   string
)

type organizeJSONItem struct {
	Repo       string `json:"repo,omitempty"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Owner      string `json:"owner"`
	Branch     string `json:"branch"`
	Head       string `json:"head"`
	Locked     bool   `json:"locked"`
	Dirty      int    `json:"dirty"`
	Ahead      int    `json:"ahead"`
	Behind     int    `json:"behind"`
	LastCommit string `json:"last_commit,omitempty"`
	PR         string `json:"pr,omitempty"`
}

var organizeCmd = &cobra.Command{
	Use:   "organize",
	Short: "Inventory every worktree, including orphans outside the pool",
	Long: `Organize answers "where did all my worktrees go?".

It lists every git worktree of the current repository — pool-managed and
scattered ones alike — with lane owner, dirty state, ahead/behind and the
pull request that branch already belongs to. Read-only by default.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			repoRoot string
			err      error
		)
		poolFor := func(repo string) string {
			cfg, cfgErr := config.Load(repo)
			if cfgErr != nil {
				return ""
			}
			resolved, rErr := config.ResolvePoolDir(repo, config.ResolveRoot(rootFlag, cfg))
			if rErr != nil {
				return ""
			}
			return resolved
		}
		if organizeRepo != "" {
			repoRoot, err = vcs.FindMainRepoRootFrom(organizeRepo)
		} else if organizeAll {
			repoRoot = organizeKok
			if repoRoot == "" {
				repoRoot, err = os.Getwd()
			}
		} else {
			repoRoot, err = vcs.FindMainRepoRoot()
		}
		if err != nil && !organizeAll {
			return fmt.Errorf("not in a git repository: %w", err)
		}

		poolDir := poolFor(repoRoot)

		if organizePrune && !organizeAll {
			if out, pErr := organize.Prune(repoRoot); pErr != nil {
				return fmt.Errorf("worktree prune failed: %w", pErr)
			} else if strings.TrimSpace(out) != "" {
				fmt.Print(out)
			}
		}

		var trees []organize.Worktree
		if organizeAll {
			trees, err = organize.ListAll(repoRoot, poolFor)
		} else {
			trees, err = organize.List(repoRoot, poolDir)
		}
		if err != nil {
			return err
		}

		prs := map[string]string{}
		if organizePR {
			for _, t := range trees {
				if t.Branch == "" || t.Branch == "(detached)" {
					continue
				}
				if pr := lookupPR(repoRoot, t.Branch); pr != "" {
					prs[t.Branch] = pr
				}
			}
		}

		if organizeJSON {
			items := make([]organizeJSONItem, 0, len(trees))
			for _, t := range trees {
				item := organizeJSONItem{
					Repo: t.Repo, Path: t.Path, Kind: t.Kind, Owner: t.Owner, Branch: t.Branch,
					Head: short(t.Head), Locked: t.Locked, Dirty: t.Dirty,
					Ahead: t.Ahead, Behind: t.Behind, PR: prs[t.Branch],
				}
				if !t.LastCommit.IsZero() {
					item.LastCommit = t.LastCommit.UTC().Format(time.RFC3339)
				}
				items = append(items, item)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(items)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if organizeAll {
			fmt.Fprintln(w, "REPO\tTÜR\tSAHİP\tDAL\tKİRLİ\tAHEAD/BEHİND\tSON\tYOL")
		} else {
			fmt.Fprintln(w, "TÜR\tSAHİP\tDAL\tKİRLİ\tAHEAD/BEHİND\tSON\tYOL")
		}
		bold := color.New(color.Bold)
		for _, t := range trees {
			dirty := strconv.Itoa(t.Dirty)
			if t.Dirty < 0 {
				dirty = "?"
			}
			son := "-"
			if !t.LastCommit.IsZero() {
				son = t.LastCommit.Format("2006-01-02 15:04")
			}
			dal := t.Branch
			if dal == "" {
				dal = "(yok)"
			}
			if p := prs[t.Branch]; p != "" {
				dal += " #" + p
			}
			if organizeAll {
				fmt.Fprintf(w, "%s\t", t.Repo)
			}
			bold.Fprintf(w, "%s", strings.ToUpper(t.Kind))
			fmt.Fprintf(w, "\t%s\t%s\t%s\t%d/%d\t%s\t%s\n", t.Owner, dal, dirty, t.Ahead, t.Behind, son, t.Path)
		}
		return w.Flush()
	},
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func lookupPR(repoRoot, branch string) string {
	slug := repoSlugRef(repoRoot)
	if slug == "" {
		return ""
	}
	out, err := exec.Command("gh", "pr", "list", "-R", slug, "--head", branch,
		"--state", "all", "--limit", "1", "--json", "number,state",
		"--jq", `.[0] | select(. != null) | "\(.number):\(.state)"`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func repoSlugRef(repoRoot string) string {
	out, err := exec.Command("git", "-C", repoRoot, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	url = strings.TrimSuffix(url, ".git")
	if i := strings.Index(url, "github.com"); i >= 0 {
		return strings.TrimPrefix(url[i+len("github.com"):], "/")
	}
	return ""
}

func init() {
	organizeCmd.Flags().BoolVar(&organizeJSON, "json", false, "emit machine-readable JSON")
	organizeCmd.Flags().BoolVar(&organizePrune, "prune", false, "drop metadata for missing worktrees before listing")
	organizeCmd.Flags().BoolVar(&organizePR, "pr", false, "attach GitHub pull-request state per branch (gh required)")
	organizeCmd.Flags().StringVar(&organizeRepo, "repo", "", "repository path (default: current directory)")
	organizeCmd.Flags().BoolVar(&organizeAll, "all", false, "inventory every git repository directly under --kok")
	organizeCmd.Flags().StringVar(&organizeKok, "kok", "", "root scanned by --all (default: current directory)")
	rootCmd.AddCommand(organizeCmd)
}
