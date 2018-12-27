// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/cmdline"
	"fuchsia.googlesource.com/jiri/project"
)

var fetchPkgsFlags struct {
	localManifest    bool
	fetchPkgsTimeout uint
	attempts         uint
}

var cmdFetchPkgs = &cmdline.Command{
	Runner: jiri.RunnerFunc(runFetchPkgs),
	Name:   "fetch-packages",
	Short:  "Fetch cipd packages using JIRI_HEAD version manifest",
	Long: `
Fetch cipd packages using local manifest JIRI_HEAD version if -local-manifest flag is
false, else it fetch cipd packages using current manifest checkout version.
`,
}

func init() {
	cmdFetchPkgs.Flags.BoolVar(&fetchPkgsFlags.localManifest, "local-manifest", false, "Use local checked out manifest.")
	cmdFetchPkgs.Flags.UintVar(&fetchPkgsFlags.fetchPkgsTimeout, "fetch-packages-timeout", project.DefaultPackageTimeout, "Timeout in minutes for fetching prebuilt packages using cipd.")
	cmdFetchPkgs.Flags.UintVar(&fetchPkgsFlags.attempts, "attempts", 1, "Number of attempts before failing.")
}

func runFetchPkgs(jirix *jiri.X, args []string) error {
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	if fetchPkgsFlags.attempts < 1 {
		return jirix.UsageErrorf("Number of attempts should be >= 1")
	}
	jirix.Attempts = fetchPkgsFlags.attempts

	// Get pkgs.
	_, _, pkgs, err := project.LoadManifestFile(jirix, jirix.JiriManifestFile(), localProjects, fetchPkgsFlags.localManifest)
	return project.FetchPackages(jirix, pkgs, fetchPkgsFlags.fetchPkgsTimeout)
}
