// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"bytes"
	"fmt"

	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/version"
)

var cmdVersion = &cmdline.Command{
	Runner: cmdline.RunnerFunc(runVersion),
	Name:   "version",
	Short:  "Print the jiri version",
	Long: `
Print the Git commit revision jiri was built from and the build date.
`,
}

func runVersion(env *cmdline.Env, args []string) error {
	var versionString bytes.Buffer
	fmt.Fprintf(&versionString, "Jiri")

	v := version.FormattedVersion()
	if v != "" {
		fmt.Fprintf(&versionString, " %s", v)
	}

	fmt.Printf("%s\n", versionString.String())

	return nil
}
