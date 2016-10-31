// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package project

import (
	"fmt"
	"path/filepath"

	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/gitutil"
	"fuchsia.googlesource.com/jiri/runutil"
	"fuchsia.googlesource.com/jiri/tool"
)

type BranchState struct {
	HasGerritMessage bool
	Name             string
}

type ProjectState struct {
	Branches                 []BranchState
	CurrentBranch            string
	CurrentTrackingBranch    string
	CurrentTrackingBranchRev string
	HasUncommitted           bool
	HasUntracked             bool
	Project                  Project
}

func setProjectState(jirix *jiri.X, state *ProjectState, checkDirty bool, ch chan<- error) {
	var err error
	scm := gitutil.New(jirix.NewSeq(), gitutil.RootDirOpt(state.Project.Path))
	var branches []string
	branches, state.CurrentBranch, err = scm.GetBranches()
	if err != nil {
		ch <- err
		return
	}
	if state.CurrentBranch != "" {
		if state.CurrentTrackingBranch, err = scm.TrackingBranchName(); err != nil {
			ch <- err
			return
		}
		if state.CurrentTrackingBranch != "" {
			if state.CurrentTrackingBranchRev, err = scm.CurrentRevisionOfBranch(state.CurrentTrackingBranch); err != nil {
				ch <- err
				return
			}
		}
	}
	for _, branch := range branches {
		file := filepath.Join(state.Project.Path, jiri.ProjectMetaDir, branch, ".gerrit_commit_message")
		hasFile := true
		if _, err := jirix.NewSeq().Stat(file); err != nil {
			if !runutil.IsNotExist(err) {
				ch <- err
				return
			}
			hasFile = false
		}
		state.Branches = append(state.Branches, BranchState{
			Name:             branch,
			HasGerritMessage: hasFile,
		})
	}
	if checkDirty {
		state.HasUncommitted, err = scm.HasUncommittedChanges()
		if err != nil {
			ch <- err
			return
		}
		state.HasUntracked, err = scm.HasUntrackedFiles()
		if err != nil {
			ch <- err
			return
		}
	}
	ch <- nil
}

func getProjectStates(jirix *jiri.X, projects Projects, checkDirty bool) (map[ProjectKey]*ProjectState, error) {
	states := make(map[ProjectKey]*ProjectState, len(projects))
	sem := make(chan error, len(projects))
	for key, project := range projects {
		state := &ProjectState{
			Project: project,
		}
		states[key] = state
		// jirix is not threadsafe, so we make a clone for each goroutine.
		go setProjectState(jirix.Clone(tool.ContextOpts{}), state, checkDirty, sem)
	}
	for _ = range projects {
		err := <-sem
		if err != nil {
			return nil, err
		}
	}
	return states, nil
}

func GetProjectStates(jirix *jiri.X, checkDirty bool) (map[ProjectKey]*ProjectState, error) {
	projects, err := LocalProjects(jirix, FastScan)
	if err != nil {
		return nil, err
	}
	return getProjectStates(jirix, projects, checkDirty)
}

func GetProjectState(jirix *jiri.X, key ProjectKey, checkDirty bool) (*ProjectState, error) {
	projects, err := LocalProjects(jirix, FastScan)
	if err != nil {
		return nil, err
	}
	sem := make(chan error, 1)
	for k, project := range projects {
		if k == key {
			state := &ProjectState{
				Project: project,
			}
			setProjectState(jirix, state, checkDirty, sem)
			return state, <-sem
		}
	}
	return nil, fmt.Errorf("failed to find project key %v", key)
}
