// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"flag"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/project"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	fetchPkgsFlags fetchPkgsCmd
	cmdFetchPkgs   = commandFromSubcommand(&fetchPkgsFlags)
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
func init() {
	fetchPkgsFlags.SetFlags(&cmdFetchPkgs.Flags)
}

type fetchPkgsCmd struct {
	localManifest     bool
	fetchPkgsTimeout  uint
	attempts          uint
	skipLocalProjects bool
	packagesToSkip    arrayFlag
}

func (c *fetchPkgsCmd) Name() string { return "fetch-packages" }
func (c *fetchPkgsCmd) Synopsis() string {
	return "Fetch cipd packages using JIRI_HEAD version manifest"
}
func (c *fetchPkgsCmd) Usage() string {
	return `
Fetch cipd packages using local manifest JIRI_HEAD version if -local-manifest flag is
false, otherwise it fetches cipd packages using current manifest checkout version.

Usage:
  jiri fetch-packages [flags]
`
}

func (c *fetchPkgsCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&c.localManifest, "local-manifest", false, "Use local checked out manifest.")
	f.UintVar(&c.fetchPkgsTimeout, "fetch-packages-timeout", project.DefaultPackageTimeout, "Timeout in minutes for fetching prebuilt packages using cipd.")
	f.UintVar(&c.attempts, "attempts", 1, "Number of attempts before failing.")
	f.BoolVar(&c.skipLocalProjects, "skip-local-projects", false, "Skip checking local project state.")
	f.Var(&runHooksFlags.packagesToSkip, "package-to-skip", "Skip fetching this package. Repeatable.")
}

func (c *fetchPkgsCmd) Execute(ctx context.Context, _ *flag.FlagSet, args ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, args)
}

func (c *fetchPkgsCmd) run(jirix *jiri.X, args []string) (err error) {
	localProjects := project.Projects{}
	if !c.skipLocalProjects {
		localProjects, err = project.LocalProjects(jirix, project.FastScan)
		if err != nil {
			return err
		}
	}
	if c.attempts < 1 {
		return jirix.UsageErrorf("Number of attempts should be >= 1")
	}
	jirix.Attempts = c.attempts

	// Get pkgs.
	var pkgs project.Packages
	if !c.localManifest {
		_, _, pkgs, err = project.LoadUpdatedManifest(jirix, localProjects, c.localManifest)
	} else {
		_, _, pkgs, err = project.LoadManifestFile(jirix, jirix.JiriManifestFile(), localProjects, c.localManifest)
	}
	if err != nil {
		return err
	}
	if err := project.FilterOptionalProjectsPackages(jirix, jirix.FetchingAttrs, nil, pkgs); err != nil {
		return err

	}
	project.FilterPackagesByName(jirix, pkgs, c.packagesToSkip)
	if len(pkgs) > 0 {
		return project.FetchPackages(jirix, pkgs, c.fetchPkgsTimeout)
	}
	return nil
}