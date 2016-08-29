// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/cmdline"
	"fuchsia.googlesource.com/jiri/project"
	"fuchsia.googlesource.com/jiri/retry"
	"fuchsia.googlesource.com/jiri/tool"
)

var (
	gcFlag       bool
	attemptsFlag int
)

func init() {
	tool.InitializeProjectFlags(&cmdUpdate.Flags)

	cmdUpdate.Flags.BoolVar(&gcFlag, "gc", false, "Garbage collect obsolete repositories.")
	cmdUpdate.Flags.IntVar(&attemptsFlag, "attempts", 1, "Number of attempts before failing.")
}

// cmdUpdate represents the "jiri update" command.
var cmdUpdate = &cmdline.Command{
	Runner: jiri.RunnerFunc(runUpdate),
	Name:   "update",
	Short:  "Update all jiri tools and projects",
	Long: `
Updates all projects, builds the latest version of all tools, and installs the
resulting binaries into $JIRI_ROOT/.jiri_root/bin. The sequence in which the
individual updates happen guarantees that we end up with a consistent set of
tools and source code. The set of projects and tools to update is described in
the manifest.

Run "jiri help manifest" for details on manifests.
`,
}

func runUpdate(jirix *jiri.X, _ []string) error {
	seq := jirix.NewSeq()
	// Create the $JIRI_ROOT/.jiri_root directory if it doesn't already exist.
	//
	// TODO(toddw): Remove this logic after the transition to .jiri_root is done.
	// The bootstrapping logic should create this directory, and jiri should fail
	// if the directory doesn't exist.
	if err := seq.MkdirAll(jirix.RootMetaDir(), 0755).Done(); err != nil {
		return err
	}

	// Update all projects to their latest version.
	// Attempt <attemptsFlag> times before failing.
	updateFn := func() error { return project.UpdateUniverse(jirix, gcFlag) }
	if err := retry.Function(jirix.Context, updateFn, retry.AttemptsOpt(attemptsFlag)); err != nil {
		return err
	}
	return project.WriteUpdateHistorySnapshot(jirix, "")
}
