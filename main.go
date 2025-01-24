// Copyright 2025 Bold Software, Inc. (https://merde.ai/)
// Released under the PolyForm Noncommercial License 1.0.0.
// Please see the README for details.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dustin/go-humanize"
)

// Overwritten by -ldflags by goreleaser for release builds.
var (
	version = "dev"
	commit  = "-"
	date    = "-"
)

func main() {
	err := rootCommand.ParseAndRun(context.Background(), os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func doRoot(ctx context.Context, args []string) error {
	cfg, err := LoadDefault(ctx)
	if err != nil {
		return err
	}
	req, err := rootRequest(ctx, cfg)
	if err != nil {
		return err
	}
	parts := doRequest(req)
	for part, err := range parts {
		if err != nil {
			return err
		}
		_, err := part.Process(ctx, cfg) // ignore binary data
		if err != nil {
			return err
		}
	}
	return nil
}

func doConfig(ctx context.Context, args []string) error {
	cfg, err := LoadDefault(ctx)
	if err != nil {
		return err
	}
	switch len(args) {
	case 0:
		for k, v := range cfg.Values {
			fmt.Printf("%s: %s\n", k, v)
		}
	case 1:
		fmt.Println(cfg.Get(args[0]))
	case 2:
		err = cfg.Update(args[0], args[1])
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("usage: merde config [key] [value]")
	}
	return nil
}

func doVersion(ctx context.Context, args []string) error {
	cfg, err := LoadDefault(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("merde version %s (%s, %s)\n", version, commit, date)
	fmt.Println(cfg.GitVersion)
	return nil
}

func doAuth(ctx context.Context, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: merde auth [token]")
	}

	cfg, err := LoadDefault(ctx)
	if err != nil {
		return err
	}

	if len(args) == 1 {
		tok := args[0]
		err := cfg.Update(tokenKey, tok)
		if err != nil {
			return err
		}
		fmt.Printf("token stored\n")
	}

	req, err := checkAuthRequest(ctx, cfg)
	if err != nil {
		return err
	}

	parts := doRequest(req)
	for part, err := range parts {
		if err != nil {
			return err
		}
		_, err := part.Process(ctx, cfg) // ignore binary data
		if err != nil {
			return err
		}
	}
	return nil
}

func doHelp(ctx context.Context, args []string) error {
	cfg, err := LoadDefault(ctx)
	if err != nil {
		return err
	}
	req, err := helpRequest(ctx, cfg, args)
	if err != nil {
		return err
	}
	parts := doRequest(req)
	for part, err := range parts {
		if err != nil {
			return err
		}
		_, err := part.Process(ctx, cfg) // ignore binary data
		if err != nil {
			return err
		}
	}
	return nil
}

func doMerge(ctx context.Context, args []string) error {
	cfg, err := LoadDefault(ctx)
	if err != nil {
		return err
	}
	// TODO: check auth before doing anything else?
	// TODO: do that concurrently with building the merge pack?
	// TODO: detect when the merge will succeed without our help and tell the user.
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("merde merge does not support flags yet")
		}
	}
	err = requireCleanGitStatus(ctx, cfg)
	if err != nil {
		return err
	}
	mainRef, topicRef, err := mainTopic(ctx, cfg, "merge", args)
	if err != nil {
		return err
	}
	fmt.Printf("plan: merge %s into %s\n", mainRef, topicRef)
	info, err := makeDeconflictRequestInfo(ctx, cfg, mainRef, topicRef)
	if err != nil {
		return err
	}
	info.verb = "merge"
	return processDeconflictRequest(ctx, cfg, info)
}

func doRebase(ctx context.Context, args []string) error {
	cfg, err := LoadDefault(ctx)
	if err != nil {
		return err
	}
	// TODO: check auth before doing anything else?
	// TODO: do that concurrently with building the merge pack?
	// TODO: detect when the rebase will succeed without our help and tell the user.
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("merde rebase does not support flags yet")
		}
	}
	err = requireCleanGitStatus(ctx, cfg)
	if err != nil {
		return err
	}
	mainRef, topicRef, err := mainTopic(ctx, cfg, "rebase", args)
	if err != nil {
		return err
	}
	fmt.Printf("plan: rebase %s onto %s\n", topicRef, mainRef)
	info, err := makeDeconflictRequestInfo(ctx, cfg, mainRef, topicRef)
	if err != nil {
		return err
	}
	info.verb = "rebase"
	return processDeconflictRequest(ctx, cfg, info)
}

// requireCleanGitStatus checks that the git status is sufficiently clean for a deconflict operation.
func requireCleanGitStatus(ctx context.Context, cfg *Config) error {
	gitDir, err := cfg.Git.GitDir(ctx)
	if err != nil {
		return err
	}
	filesReason := map[string]string{
		"MERGE_HEAD":       "merge is in progress",
		"REBASE_HEAD":      "rebase is in progress",
		"CHERRY_PICK_HEAD": "cherry-pick is in progress",
		"REVERT_HEAD":      "revert is in progress",
	}
	for file, reason := range filesReason {
		_, err := os.Stat(filepath.Join(gitDir, file))
		if err == nil {
			return fmt.Errorf("cannot proceed: %s", reason)
		}
	}
	return nil
}

// mainTopic returns the main and topic refs, given args.
func mainTopic(ctx context.Context, cfg *Config, verb string, args []string) (string, string, error) {
	var mainRef, topicRef string
	switch len(args) {
	case 0:
		// handled below
	case 1:
		mainRef = args[0]
	case 2:
		// TODO: we could support merging two branches,
		// but it would have different semantics from "git merge X Y",
		// which does an octopus merge, so for now, tread lightly.
		if verb == "merge" {
			return "", "", fmt.Errorf("merde merge takes at most 1 argument")
		}
		mainRef = args[0]
		topicRef = args[1]
	default:
		return "", "", fmt.Errorf("too many arguments to merde %v", verb)
	}
	if topicRef == "" {
		abbrev, err := cfg.Git.AbbrevRef(ctx, "HEAD")
		if err != nil {
			return "", "", err
		}
		topicRef = abbrev
	}
	if mainRef == "" {
		hasUpstream, err := cfg.Git.HasUpstream(ctx, topicRef)
		if err != nil {
			return "", "", err
		}
		if !hasUpstream {
			return "", "", fmt.Errorf("no upstream set for %s, please explicitly specify a main branch: merde %s <main>", topicRef, verb)
		}
		upstream, err := cfg.Git.AbbrevRef(ctx, topicRef+"@{upstream}")
		if err != nil {
			return "", "", err
		}
		mainRef = upstream
	}
	return mainRef, topicRef, nil
}

type deconflictRequestInfo struct {
	verb     string   // "merge" or "rebase"
	args     []string // args associated with verb, placeholder for now
	mainRef  string   // e.g. "main" or "origin/main"
	topicRef string   // e.g. "topic" or "main"
	mainSHA  string   // commit hash of mainRef
	topicSHA string   // commit hash of topicRef
	pack     string   // pack file of objects needed to analyze and combine the two branches
}

func makeDeconflictRequestInfo(ctx context.Context, cfg *Config, mainRef, topicRef string) (*deconflictRequestInfo, error) {
	mainSHA, err := cfg.Git.ResolveRef(ctx, mainRef)
	if err != nil {
		return nil, err
	}
	topicSHA, err := cfg.Git.ResolveRef(ctx, topicRef)
	if err != nil {
		return nil, err
	}
	fmt.Printf("analyzing...\n")
	// TODO: this can be slow, might need a spinner
	pack, err := cfg.Git.MergePack(ctx, mainSHA, topicSHA)
	if err != nil {
		return nil, err
	}
	info := &deconflictRequestInfo{
		mainRef:  mainRef,
		topicRef: topicRef,
		mainSHA:  mainSHA,
		topicSHA: topicSHA,
		pack:     pack,
	}
	return info, nil
}

func processDeconflictRequest(ctx context.Context, cfg *Config, info *deconflictRequestInfo) error {
	dr, err := deconflictRequest(ctx, cfg, info)
	if err != nil {
		return err
	}
	fmt.Printf("uploading %v...\n", humanize.Bytes(uint64(len(info.pack))))
	parts := doRequest(dr)
	for part, err := range parts {
		if err != nil {
			return err
		}
		done, err := part.Process(ctx, cfg)
		if err != nil {
			return err
		}
		if !done {
			// binary data, unpack git objects
			err = cfg.Git.UnpackObjects(ctx, part.Data)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
