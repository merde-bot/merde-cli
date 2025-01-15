// Copyright 2025 Bold Software, Inc. (https://merde.ai/)
// Released under the PolyForm Noncommercial License 1.0.0.
// Please see the README for details.

package main

import (
	"flag"

	"github.com/peterbourgon/ff/v3/ffcli"
)

var (
	rootFlagSet = flag.NewFlagSet("merde", flag.ContinueOnError)

	rootCommand = &ffcli.Command{
		Name:        "merde",
		ShortUsage:  "merde [flags] <subcommand>",
		ShortHelp:   "merde.ai client",
		FlagSet:     rootFlagSet,
		Exec:        doRoot,
		Subcommands: []*ffcli.Command{authCommand, versionCommand, configCommand, helpCommand, mergeCommand, rebaseCommand},
	}

	versionCommand = &ffcli.Command{
		Name:       "version",
		ShortUsage: "merde version",
		ShortHelp:  "print version information and exit",
		Exec:       doVersion,
	}

	configCommand = &ffcli.Command{
		Name:       "config",
		ShortUsage: "merde config [key] [value]",
		ShortHelp:  "get/set config values (low level, for debugging/development)",
		Exec:       doConfig,
	}

	authCommand = &ffcli.Command{
		Name:       "auth",
		ShortUsage: "merde auth [token]",
		ShortHelp:  "(re-)authenticate",
		Exec:       doAuth,
	}

	helpCommand = &ffcli.Command{
		Name:       "help",
		ShortUsage: "merde help",
		ShortHelp:  "print detailed usage information",
		Exec:       doHelp,
	}

	mergeCommand = &ffcli.Command{
		Name:       "merge",
		ShortUsage: "merde merge [topic]",
		ShortHelp:  "merge <topic> into current branch; topic defaults to the current upstream",
		Exec:       doMerge,
	}

	rebaseCommand = &ffcli.Command{
		Name:       "rebase",
		ShortUsage: "merde rebase [main-branch [topic-branch]]",
		ShortHelp:  "rebase <topic> atop <main>; topic defaults to the current branch and main defaults to its upstream",
		Exec:       doRebase,
	}
)
