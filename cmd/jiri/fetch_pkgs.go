// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/project"
)

var fetchPkgsFlags struct {
	localManifest     bool
	fetchPkgsTimeout  uint
	attempts          uint
	skipLocalProjects bool
	packagesToSkip    arrayFlag
}

var cmdFetchPkgs = &cmdline.Command{
	Runner: jiri.RunnerFunc(runFetchPkgs),
	Name:   "fetch-packages",
	Short:  "Fetch cipd packages using JIRI_HEAD version manifest",
	Long: `
Fetch cipd packages using local manifest JIRI_HEAD version if -local-manifest flag is
false, otherwise it fetches cipd packages using current manifest checkout version.
`,
}

func init() {
	cmdFetchPkgs.Flags.BoolVar(&fetchPkgsFlags.localManifest, "local-manifest", false, "Use local checked out manifest.")
	cmdFetchPkgs.Flags.UintVar(&fetchPkgsFlags.fetchPkgsTimeout, "fetch-packages-timeout", project.DefaultPackageTimeout, "Timeout in minutes for fetching prebuilt packages using cipd.")
	cmdFetchPkgs.Flags.UintVar(&fetchPkgsFlags.attempts, "attempts", 1, "Number of attempts before failing.")
	cmdFetchPkgs.Flags.BoolVar(&fetchPkgsFlags.skipLocalProjects, "skip-local-projects", false, "Skip checking local project state.")
	cmdFetchPkgs.Flags.Var(&runHooksFlags.packagesToSkip, "package-to-skip", "Skip fetching this package. Repeatable.")
}

func runFetchPkgs(jirix *jiri.X, args []string) (err error) {
	localProjects := project.Projects{}
	if !fetchPkgsFlags.skipLocalProjects {
		localProjects, err = project.LocalProjects(jirix, project.FastScan)
		if err != nil {
			return err
		}
	}
	if fetchPkgsFlags.attempts < 1 {
		return jirix.UsageErrorf("Number of attempts should be >= 1")
	}
	jirix.Attempts = fetchPkgsFlags.attempts

	// Get pkgs.
	var pkgs project.Packages
	if !fetchPkgsFlags.localManifest {
		_, _, pkgs, err = project.LoadUpdatedManifest(jirix, localProjects, fetchPkgsFlags.localManifest)
	} else {
		_, _, pkgs, err = project.LoadManifestFile(jirix, jirix.JiriManifestFile(), localProjects, fetchPkgsFlags.localManifest)
	}
	if err != nil {
		return err
	}
	if err := project.FilterOptionalProjectsPackages(jirix, jirix.FetchingAttrs, nil, pkgs); err != nil {
		return err
	}
	project.FilterPackagesByName(jirix, pkgs, fetchPkgsFlags.packagesToSkip)
	if len(pkgs) > 0 {
		return project.FetchPackages(jirix, pkgs, fetchPkgsFlags.fetchPkgsTimeout)
	}
	return nil
}
