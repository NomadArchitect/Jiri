// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"

	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/collect"
	"fuchsia.googlesource.com/jiri/project"
	"v.io/x/lib/cmdline"
)

// cmdRebuild represents the "jiri rebuild" command.
var cmdRebuild = &cmdline.Command{
	Runner: jiri.RunnerFunc(runRebuild),
	Name:   "rebuild",
	Short:  "Rebuild all jiri tools",
	Long: `
Rebuilds all jiri tools and installs the resulting binaries into
$JIRI_ROOT/.jiri_root/bin. This is similar to "jiri update", but does not update
any projects before building the tools. The set of tools to rebuild is described
in the manifest.

Run "jiri help manifest" for details on manifests.
`,
}

func runRebuild(jirix *jiri.X, args []string) (e error) {
	projects, tools, err := project.LoadManifest(jirix)
	if err != nil {
		return err
	}

	// Create a temporary directory in which tools will be built.
	tmpDir, err := jirix.NewSeq().TempDir("", "tmp-jiri-rebuild")
	if err != nil {
		return fmt.Errorf("TempDir() failed: %v", err)
	}

	// Make sure we cleanup the temp directory.
	defer collect.Error(func() error { return jirix.NewSeq().RemoveAll(tmpDir).Done() }, &e)

	// Paranoid sanity checking.
	if _, ok := tools[project.JiriName]; !ok {
		return fmt.Errorf("tool %q not found", project.JiriName)
	}

	// Build and install tools.
	if err := project.BuildTools(jirix, projects, tools, tmpDir); err != nil {
		return err
	}
	return project.InstallTools(jirix, tmpDir)
}
