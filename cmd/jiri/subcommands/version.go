// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"bytes"
	"context"
	"flag"
	"fmt"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/version"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	versionFlags versionCmd
	cmdVersion   = &cmdline.Command{
		Name:   versionFlags.Name(),
		Short:  versionFlags.Synopsis(),
		Long:   versionFlags.Usage(),
		Runner: cmdline.RunnerFunc(versionFlags.run),
	}
)

type versionCmd struct {
	cmdBase
}

func (c *versionCmd) Name() string     { return "version" }
func (c *versionCmd) Synopsis() string { return "Print the jiri version" }
func (c *versionCmd) Usage() string {
	return `Print the Git commit revision jiri was built from and the build date.

Usage:
  jiri version
`
}

func (c *versionCmd) SetFlags(f *flag.FlagSet) {
	c.topLevelFlags.SetFlags(f)
}

func (c *versionCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return errToExitStatus(c.run(ctx, f.Args()))
}

func (c *versionCmd) run(ctx context.Context, args []string) error {
	var versionString bytes.Buffer
	fmt.Fprintf(&versionString, "Jiri")

	v := version.FormattedVersion()
	if v != "" {
		fmt.Fprintf(&versionString, " %s", v)
	}

	fmt.Printf("%s\n", versionString.String())

	return nil
}
