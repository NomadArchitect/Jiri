// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package project

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"

	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/git"
)

const (
	SourceManifestVersion = int32(0)
)

type SourceManifestGitCheckout struct {
	// The canonicalized URL of the original repo that is considered the “source
	// of truth” for the source code. Ex.
	//   https://chromium.googlesource.com/chromium/tools/build.git
	//   https://github.com/luci/recipes-py
	RepoUrl string `json:"repo_url,omitempty"`

	// If different from repo_url, this can be the URL of the repo that the source
	// was actually fetched from (i.e. a mirror). Ex.
	//   https://chromium.googlesource.com/external/github.com/luci/recipes-py
	//
	// If this is empty, it's presumed to be equal to repo_url.
	FetchUrl string `json:"fetch_url,omitempty"`

	// The fully resolved revision (commit hash) of the source. Ex.
	//   3617b0eea7ec74b8e731a23fed2f4070cbc284c4
	//
	// Note that this is the raw revision bytes, not their hex-encoded form.
	Revision []byte `json:"revision,omitempty"`

	// The ref that the task used to resolve the revision of the source (if any). Ex.
	//   refs/heads/master
	//   refs/changes/04/511804/4
	//
	// This should always be a ref on the hosted repo (not any local alias
	// like 'refs/remotes/...').
	//
	// This should always be an absolute ref (i.e. starts with 'refs/'). An
	// example of a non-absolute ref would be 'master'.
	TrackingRef string `json:"tracking_ref,omitempty"`
}

type SourceManifestDirectory struct {
	GitCheckout *SourceManifestGitCheckout `json:"git_checkout,omitempty"`
}

type SourceManifest struct {
	// Version will increment on backwards-incompatible changes only. Backwards
	// compatible changes will not alter this version number.
	//
	// Currently, the only valid version number is 0.
	Version int32 `json:"version"`

	// Map of local file system directory path (with forward slashes) to
	// a Directory message containing one or more deployments.
	//
	// The local path is relative to some job-specific root. This should be used
	// for informational/display/organization purposes, and should not be used as
	// a global primary key. i.e. if you depend on chromium/src.git being in
	// a folder called “src”, I will find you and make really angry faces at you
	// until you change it...（╬ಠ益ಠ). Instead, implementations should consider
	// indexing by e.g. git repository URL or cipd package name as more better
	// primary keys.
	Directories map[string]*SourceManifestDirectory `json:"directories"`
}

func NewSourceManifest(jirix *jiri.X, projects Projects) (*SourceManifest, error) {
	p := make([]Project, len(projects))
	i := 0
	for _, proj := range projects {
		if err := proj.relativizePaths(jirix.Root); err != nil {
			return nil, err
		}
		p[i] = proj
		i++
	}
	sm := &SourceManifest{
		Version:     SourceManifestVersion,
		Directories: make(map[string]*SourceManifestDirectory),
	}
	sort.Sort(ProjectsByPath(p))
	for _, proj := range p {
		gc := &SourceManifestGitCheckout{
			RepoUrl: proj.Remote,
		}
		g := git.NewGit(filepath.Join(jirix.Root, proj.Path))
		if rev, err := g.CurrentRevisionRaw(); err != nil {
			return nil, err
		} else {
			gc.Revision = rev
		}
		if proj.RemoteBranch != "" {
			proj.RemoteBranch = "master"
		}
		gc.TrackingRef = "refs/heads/" + proj.RemoteBranch
		sm.Directories[proj.Path] = &SourceManifestDirectory{GitCheckout: gc}
	}
	return sm, nil
}

func (sm *SourceManifest) ToFile(jirix *jiri.X, filename string) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmtError(err)
	}
	out, err := json.MarshalIndent(sm, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize JSON output: %s\n", err)
	}

	err = ioutil.WriteFile(filename, out, 0600)
	if err != nil {
		return fmt.Errorf("failed write JSON output to %s: %s\n", filename, err)
	}

	return nil
}
