// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.fuchsia.dev/jiri/jiritest/xtest"
)

type importTestCase struct {
	Args           []string
	Filename       string
	OutputFileName string
	Exist, Want    string
	Stdout, Stderr string
	SetFlags       func()
	runOnce        bool
}

func setDefaultImportFlags() {
	importFlags.name = "manifest"
	importFlags.remoteBranch = "main"
	importFlags.revision = ""
	importFlags.root = ""
	importFlags.overwrite = false
	importFlags.out = ""
	importFlags.delete = false
	importFlags.list = false
	importFlags.jsonOutput = ""
}

func TestImport(t *testing.T) {
	tests := []importTestCase{
		{
			Stderr: `wrong number of arguments`,
		},
		{
			Args:   []string{"a"},
			Stderr: `wrong number of arguments`,
		},
		{
			Args:   []string{"a", "b", "c"},
			Stderr: `wrong number of arguments`,
		},
		// Remote imports, default append behavior
		{
			SetFlags: func() {
				importFlags.name = "name"
				importFlags.remoteBranch = "remotebranch"
				importFlags.root = "root"
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="name" remote="https://github.com/new.git" remotebranch="remotebranch" root="root"/>
  </imports>
</manifest>
`,
		},
		{
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			SetFlags: func() {
				importFlags.out = "file"
			},
			Args:     []string{"foo", "https://github.com/new.git"},
			Filename: `file`,
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			SetFlags: func() {
				importFlags.out = "-"
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Stdout: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			SetFlags: func() {
				importFlags.list = true
				importFlags.jsonOutput = "file"
			},
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			OutputFileName: `file`,
			Want: `[
  {
    "manifest": "bar",
    "name": "manifest",
    "remote": "https://github.com/orig.git",
    "revision": "",
    "remoteBranch": "",
    "root": ""
  }
]`,
		},
		{
			SetFlags: func() {
				importFlags.list = true
			},
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest_bar" remote="https://github.com/bar.git"/>
	<import manifest="foo" name="manifest_foo" remote="https://github.com/foo.git"/>
  </imports>
</manifest>
`,
			Stdout: `* import	manifest_bar
  Manifest:	bar
  Remote:	https://github.com/bar.git
  Revision:	
  RemoteBranch:	
  Root:	
* import	manifest_foo
  Manifest:	foo
  Remote:	https://github.com/foo.git
  Revision:	
  RemoteBranch:	
  Root:	
`,
		},
		{
			Args: []string{"foo", "https://github.com/new.git"},
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		// Remote imports, explicit overwrite behavior
		{
			SetFlags: func() {
				importFlags.overwrite = true
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			SetFlags: func() {
				importFlags.overwrite = true
				importFlags.out = "file"
			},
			Args:     []string{"foo", "https://github.com/new.git"},
			Filename: `file`,
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			SetFlags: func() {
				importFlags.overwrite = true
				importFlags.out = "-"
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Stdout: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			SetFlags: func() {
				importFlags.overwrite = true
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		// test delete flag
		{
			SetFlags: func() {
				importFlags.delete = true
			},
			Stderr:  `wrong number of arguments`,
			runOnce: true,
		},
		{
			SetFlags: func() {
				importFlags.delete = true
			},
			Args:    []string{"a", "b", "c"},
			Stderr:  `wrong number of arguments`,
			runOnce: true,
		},
		{
			SetFlags: func() {
				importFlags.delete = true
				importFlags.overwrite = true
			},
			Args:    []string{"a", "b"},
			Stderr:  `cannot use -delete and -overwrite together`,
			runOnce: true,
		},
		{
			SetFlags: func() {
				importFlags.delete = true
			},
			Args:    []string{"foo"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo1" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo1" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
		},
		{
			SetFlags: func() {
				importFlags.delete = true
			},
			Args:    []string{"foo"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github1.com/orig.git"/>
  </imports>
</manifest>
`,
			Stderr: `More than 1 import meets your criteria. Please provide remote.`,
		},
		{
			SetFlags: func() {
				importFlags.delete = true
			},
			Args:    []string{"foo"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
		},
		{
			SetFlags: func() {
				importFlags.delete = true
			},
			Args:    []string{"foo", "https://github2.com/orig.git"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github2.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
		},
	}

	for _, test := range tests {
		if err := testImport(t, test); err != nil {
			t.Errorf("%v: %v", test.Args, err)
		}
	}
}

func testImport(t *testing.T, test importTestCase) error {
	jirix := xtest.NewX(t)

	// Temporary directory in which to run `jiri import`.
	tmpDir := t.TempDir()

	// Allow optional non-default filenames, for testing the -out option.
	filename := test.Filename
	if filename == "" {
		filename = ".jiri_manifest"
	}

	// Set up manfile for the local file import tests.  It should exist in both
	// the tmpDir (for ../manfile tests) and jiriRoot.
	for _, dir := range []string{tmpDir, jirix.Root} {
		if err := os.WriteFile(filepath.Join(dir, "manfile"), nil, 0644); err != nil {
			return err
		}
	}

	// Set up an existing file if it was specified.
	if test.Exist != "" {
		if err := os.WriteFile(filename, []byte(test.Exist), 0644); err != nil {
			return err
		}
	}

	run := func() error {
		// Run import and check the results.
		var err error
		importCmd := func() {
			setDefaultImportFlags()
			if test.SetFlags != nil {
				test.SetFlags()
			}
			err = runImport(jirix, test.Args)
		}
		stdout, _, runErr := runfunc(importCmd)
		if runErr != nil {
			return err
		}
		stderr := ""
		if err != nil {
			stderr = err.Error()
		}
		if got, want := stdout, test.Stdout; !strings.Contains(got, want) || (got != "" && want == "") {
			return fmt.Errorf("stdout got %q, want substr %q", got, want)
		}
		if got, want := stderr, test.Stderr; !strings.Contains(got, want) || (got != "" && want == "") {
			return fmt.Errorf("stderr got %q, want substr %q", got, want)
		}
		return nil
	}
	if err := run(); err != nil {
		return err
	}

	// check that it is idempotent
	if !test.runOnce {
		if err := run(); err != nil {
			return err
		}
	}
	f := test.OutputFileName
	if f == "" {
		f = filename
	}

	// Make sure the right file is generated.
	if test.Want != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if got, want := string(data), test.Want; got != want {
			return fmt.Errorf("GOT\n%s\nWANT\n%s", got, want)
		}
	}
	return nil
}
