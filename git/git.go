// Copyright 2025 Bold Software, Inc. (https://merde.ai/)
// Released under the PolyForm Noncommercial License 1.0.0.
// Please see the README for details.

package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/josharian/xc"
)

type Git struct {
	bin string
}

func NewGit(bin string) (*Git, error) {
	if bin != "" {
		return &Git{bin: bin}, nil
	}
	for _, gitExe := range []string{"git", "git.exe"} {
		bin, err := exec.LookPath(gitExe)
		if err == nil {
			return &Git{bin: bin}, nil
		}
	}
	return nil, fmt.Errorf("git[.exe] not found in PATH")
}

// baseCommand constructs an xc git command.
func (g *Git) baseCommand(ctx context.Context) *xc.Builder {
	return xc.Command(ctx, g.bin)
}

func (g *Git) Version(ctx context.Context) (string, error) {
	return g.baseCommand(ctx).
		AppendArgs("--version").
		Describe("get git version").
		Run().
		TrimSpace().
		String()
}

func (g *Git) GitDir(ctx context.Context) (string, error) {
	return g.baseCommand(ctx).
		AppendArgs("rev-parse", "--git-dir").
		Describe("get git dir").
		Run().
		TrimSpace().
		String()
}

// Remotes returns all remote urls.
func (g *Git) Remotes(ctx context.Context) ([]string, error) {
	remotes, err := g.baseCommand(ctx).
		AppendArgs("remote").
		Describef("list remotes").
		Run().
		TrimSpace().
		Split("\n")
	if err != nil {
		return nil, err
	}
	var all []string
	for _, remote := range remotes {
		urls, err := g.baseCommand(ctx).
			AppendArgs("remote", "get-url", "--all", remote).
			Describef("get URLs for remote %s", remote).
			Run().
			TrimSpace().
			Split("\n")
		if err != nil {
			continue
		}
		for _, u := range urls {
			if !strings.Contains(u, "github.com") && !strings.Contains(u, "gitlab.com") {
				continue
			}
			// Quadratic but simpler, and nobody has _that_ many remotes. Right?
			if !slices.Contains(all, u) {
				all = append(all, u)
			}
		}
	}
	return all, nil
}

// MergeBases returns the merge bases of the given commits.
func (g *Git) MergeBases(ctx context.Context, commits []string) ([]string, error) {
	return g.baseCommand(ctx).
		AppendArgs("merge-base", "--all").
		AppendArgs(commits...).
		Describef("get merge bases for %v", commits).
		Run().
		TrimSpace().
		Split("\n")
}

// UniqueAncestorMergeBase recursively finds merge bases of the given commits until there is only one.
// If there is no unique merge base, it returns "", nil.
func (g *Git) UniqueAncestorMergeBase(ctx context.Context, commits []string) (string, error) {
	for {
		bases, err := g.MergeBases(ctx, commits)
		if err != nil {
			return "", err
		}
		switch len(bases) {
		case 0:
			return "", nil
		case 1:
			return bases[0], nil
		}
		commits = bases
	}
}

// ResolveRef resolves a refName to a commit hash.
// If the refName is not found, it returns an error.
func (g *Git) ResolveRef(ctx context.Context, refName string) (string, error) {
	return g.baseCommand(ctx).
		AppendArgs("rev-parse", refName).
		Run().
		TrimSpace().
		String()
}

// CreateRef creates refName pointing to sha.
// If the ref already exists, it returns an error.
func (g *Git) CreateRef(ctx context.Context, refName, sha string) error {
	return g.baseCommand(ctx).
		AppendArgs("update-ref", "--stdin", "-z").
		StdinString(fmt.Sprintf("create %s\000%s\000", refName, sha)).
		Run().
		Wait()
}

// Upstream returns the upstream of the given ref.
// If the ref has no upstream, it returns an "", nil.
// A non-nil error only occurs if git fails in an unexpected way.
func (g *Git) HasUpstream(ctx context.Context, refName string) (bool, error) {
	out, err := g.baseCommand(ctx).
		AppendArgs("rev-parse", "--verify", refName+"@{upstream}").
		Run().
		TrimSpace().
		AllowExitCodes(128).
		String()
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// AbbrevRef resolves a refName to a short, unambiguous ref.
// If the refName cannot be shortened, it resolves it to a commit hash and returns that.
// If the refName cannot be resolved, it returns an error.
func (g *Git) AbbrevRef(ctx context.Context, refName string) (string, error) {
	out, err := g.baseCommand(ctx).
		AppendArgs("rev-parse", "--abbrev-ref=loose", refName).
		Run().
		TrimSpace().
		String()
	if err != nil {
		return "", err
	}
	if out == "" || out == refName {
		return g.ResolveRef(ctx, refName)
	}
	return out, nil
}

// commitsBetween returns the commits contained in tips but not in base.
// It includes base.
func (g *Git) commitsBetween(ctx context.Context, base string, tips []string) ([]string, error) {
	commits, err := g.baseCommand(ctx).
		AppendArgs("rev-list").
		AppendArgs(tips...).
		AppendArgs("--not", base).
		Describef("get commits between %v and %s", tips, base).
		Run().
		TrimSpace().
		Split("\n")
	if err != nil {
		return nil, err
	}
	commits = append(commits, base)
	return commits, nil
}

func (g *Git) treesReferenced(ctx context.Context, commits []string) ([]string, error) {
	batch := new(bytes.Buffer)
	for _, commit := range commits {
		fmt.Fprintf(batch, "%s^{tree}\n", commit)
	}
	return g.baseCommand(ctx).
		AppendArgs("cat-file", "--buffer", "--batch-check=%(objectname)").
		Describef("get trees referenced by %v", commits).
		Stdin(batch).
		Run().
		TrimSpace().
		Split("\n")
}

// varyingPaths returns the objects that correspond to different contents at the same path between the given trees.
func (g *Git) varyingPaths(ctx context.Context, trees []string) ([]string, error) {
	type contents struct {
		typ    string // blob or tree or commit
		sha    string // sha of the object
		varies bool   // known to vary?
	}
	pathContents := make(map[string]contents)
	var varying []string
	for _, tree := range trees {
		lines, err := g.baseCommand(ctx).
			AppendArgs("ls-tree", "-r", "-t", "-z", "--format=%(objecttype) %(objectname) %(path)", tree).
			Describef("getting paths in %s", tree).
			Run().
			Split("\x00")
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, " ", 3)
			if len(parts) != 3 {
				return nil, fmt.Errorf("unexpected line: %s", line)
			}
			typ, sha, path := parts[0], parts[1], parts[2]
			switch typ {
			case "blob", "tree", "commit":
			default:
				return nil, fmt.Errorf("unexpected object type: %s", typ)
			}
			if path == "" {
				return nil, fmt.Errorf("unexpected empty path")
			}
			if len(sha) != 40 {
				return nil, fmt.Errorf("unexpected sha length: %d", len(sha))
			}
			c := pathContents[path]
			// first object for any path is a freebie
			if c.typ == "" {
				c.typ = typ
				c.sha = sha
				pathContents[path] = c
				continue
			}
			if c.varies {
				varying = append(varying, sha)
				continue
			}
			// if there are any mismatches, it varies
			if c.typ != typ || c.sha != sha {
				if c.typ == "commit" || typ == "commit" {
					return nil, fmt.Errorf("changes involving submodules are not supported")
				}
				c.varies = true
				varying = append(varying, c.sha, sha)
				pathContents[path] = c
				continue
			}
			// otherwise, it's the same
		}
	}
	return varying, nil
}

func (g *Git) packObjects(ctx context.Context, objects []string) (string, error) {
	packList := new(bytes.Buffer)
	for _, obj := range objects {
		packList.WriteString(obj)
		packList.WriteByte('\n')
	}
	return g.baseCommand(ctx).
		AppendArgs("pack-objects", "--stdout", "--delta-base-offset", "-q").
		Stdin(packList).
		Describef("packing %v objects", len(objects)).
		Run().
		String()
}

func (g *Git) MergePack(ctx context.Context, main, topic string) (string, error) {
	base, err := g.UniqueAncestorMergeBase(ctx, []string{main, topic})
	if err != nil {
		return "", err
	}
	commits, err := g.commitsBetween(ctx, base, []string{main, topic})
	if err != nil {
		return "", err
	}
	// fmt.Println("n commits:", len(commits))
	trees, err := g.treesReferenced(ctx, commits)
	if err != nil {
		return "", err
	}
	// fmt.Println("n trees:", len(trees))
	varying, err := g.varyingPaths(ctx, trees)
	if err != nil {
		return "", err
	}
	var need []string
	need = append(need, commits...)
	need = append(need, trees...)
	need = append(need, varying...)
	// fmt.Println("n varying:", len(varying))
	pack, err := g.packObjects(ctx, need)
	if err != nil {
		return "", err
	}
	// fmt.Println("pack size", len(pack))
	return pack, nil
}

func (g *Git) UnpackObjects(ctx context.Context, pack *bytes.Buffer) error {
	return g.baseCommand(ctx).
		AppendArgs("unpack-objects", "-q").
		Stdin(pack).
		Describef("unpacking %d bytes worth of objects", pack.Len()).
		Run().
		Wait()
}
