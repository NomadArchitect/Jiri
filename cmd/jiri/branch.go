// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/cmdline"
	"fuchsia.googlesource.com/jiri/git"
	"fuchsia.googlesource.com/jiri/gitutil"
	"fuchsia.googlesource.com/jiri/project"
)

var branchFlags struct {
	deleteFlag       bool
	forceDeleteFlag  bool
	listFlag         bool
	deleteMergedFlag bool
}

var cmdBranch = &cmdline.Command{
	Runner: jiri.RunnerFunc(runBranch),
	Name:   "branch",
	Short:  "Show or delete branches",
	Long: `
Show all the projects having branch <branch> .If -d or -D is passed, <branch>
is deleted. if <branch> is not passed, show all projects which have branches other than "master"`,
	ArgsName: "<branch>",
	ArgsLong: "<branch> is the name branch",
}

func init() {
	flags := &cmdBranch.Flags
	flags.BoolVar(&branchFlags.deleteFlag, "d", false, "Delete branch from project. Similar to running 'git branch -d <branch-name>'")
	flags.BoolVar(&branchFlags.forceDeleteFlag, "D", false, "Force delete branch from project. Similar to running 'git branch -D <branch-name>'")
	flags.BoolVar(&branchFlags.listFlag, "list", false, "Show only projects with current branch <branch>")
	flags.BoolVar(&branchFlags.deleteMergedFlag, "delete-merged", false, "Delete merged branches. Merged branches are the tracked branches merged with their tracking remote or un-tracked branches merged with the branch specified in manifest(default master). If <branch> is provided, it will only delete branch <branch> if merged.")
}

func displayProjects(jirix *jiri.X, branch string) error {
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	jirix.TimerPush("Get states")
	states, err := project.GetProjectStates(jirix, localProjects, false)
	if err != nil {
		return err
	}

	jirix.TimerPop()
	cDir, err := os.Getwd()
	if err != nil {
		return err
	}
	var keys project.ProjectKeys
	for key, _ := range states {
		keys = append(keys, key)
	}
	sort.Sort(keys)
	for _, key := range keys {
		state := states[key]
		relativePath, err := filepath.Rel(cDir, state.Project.Path)
		if err != nil {
			return err
		}
		if branch == "" {
			var branches []string
			master := ""
			for _, b := range state.Branches {
				name := b.Name
				if state.CurrentBranch.Name == b.Name {
					name = "*" + jirix.Color.Green("%s", b.Name)
				}
				if b.Name != "master" {
					branches = append(branches, name)
				} else {
					master = name
				}
			}
			if len(branches) != 0 {
				if master != "" {
					branches = append(branches, master)
				}
				fmt.Printf("%s: %s(%s)\n", jirix.Color.Yellow("Project"), state.Project.Name, relativePath)
				fmt.Printf("%s: %s\n\n", jirix.Color.Yellow("Branch(es)"), strings.Join(branches, ", "))
			}

		} else if branchFlags.listFlag {
			if state.CurrentBranch.Name == branch {
				fmt.Printf("%s(%s)\n", state.Project.Name, relativePath)
			}
		} else {
			for _, b := range state.Branches {
				if b.Name == branch {
					fmt.Printf("%s(%s)\n", state.Project.Name, relativePath)
					break
				}
			}
		}
	}
	jirix.TimerPop()
	return nil
}

func runBranch(jirix *jiri.X, args []string) error {
	branch := ""
	if len(args) > 1 {
		return jirix.UsageErrorf("Please provide only one branch")
	} else if len(args) == 1 {
		branch = args[0]
	}
	if branchFlags.deleteFlag || branchFlags.forceDeleteFlag {
		if branch == "" {
			return jirix.UsageErrorf("Please provide branch to delete")
		}
		return deleteBranches(jirix, branch)
	}
	if branchFlags.deleteMergedFlag {
		return deleteMergedBranches(jirix, branch)
	}
	return displayProjects(jirix, branch)
}

func deleteMergedBranches(jirix *jiri.X, branchToDelete string) error {
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	cDir, err := os.Getwd()
	if err != nil {
		return err
	}
	jirix.TimerPush("Get states")
	states, err := project.GetProjectStates(jirix, localProjects, false)
	if err != nil {
		return err
	}
	jirix.TimerPop()
	remoteProjects, _, err := project.LoadManifestFile(jirix, jirix.JiriManifestFile(), localProjects, false /*localManifest*/)
	if err != nil {
		return err
	}
	jirix.TimerPush("Process")
	for key, state := range states {
		remote, ok := remoteProjects[key]
		relativePath, err := filepath.Rel(cDir, state.Project.Path)
		if err != nil {
			relativePath = state.Project.Path
		}
		if !ok {
			jirix.Logger.Debugf("Not processing project %s(%s) as it was not found in manifest\n\n", state.Project.Name, relativePath)
			continue
		}
		deletedBranches, err := deleteProjectMergedBranches(jirix, state, remote, relativePath, branchToDelete)
		if len(deletedBranches) != 0 || err != nil {
			buf := fmt.Sprintf("Project: %s(%s)\n", state.Project.Name, relativePath)
			if len(deletedBranches) != 0 {
				buf = buf + fmt.Sprintf("%s: %s\n", jirix.Color.Green("Deleted branch(es)"), strings.Join(deletedBranches, ","))
				for _, b := range deletedBranches {
					if b == state.CurrentBranch.Name {
						buf = buf + fmt.Sprintf("Current branch \"%s\" was deleted and project was put on JIRI_HEAD\n", jirix.Color.Yellow(b))
					}
				}
			}
			if err != nil {
				jirix.IncrementFailures()
				buf = buf + fmt.Sprintf("%s", err)
				jirix.Logger.Errorf("%s\n", buf)
			} else {
				jirix.Logger.Infof("%s\n", buf)
			}
		}
	}

	if jirix.Failures() != 0 {
		return fmt.Errorf("Branch deletion completed with non-fatal errors.")
	}
	return nil
}

func deleteProjectMergedBranches(jirix *jiri.X, state *project.ProjectState, remote project.Project, relativePath, branchToDelete string) ([]string, error) {
	deletedBranches := []string{}
	var retErr error
	var mergedBranches map[string]bool
	scm := gitutil.New(jirix, gitutil.RootDirOpt(state.Project.Path))
	g := git.NewGit(state.Project.Path)
	for _, b := range state.Branches {
		if branchToDelete != "" && b.Name != branchToDelete {
			continue
		}
		deleteForced := false

		if b.Tracking == nil {
			// check if this branch is merged
			if mergedBranches == nil {
				// populate
				mergedBranches = make(map[string]bool)
				rb := remote.RemoteBranch
				if rb == "" {
					rb = "master"
				}
				if mbs, err := g.MergedBranches("remotes/origin/" + rb); err != nil {
					retErr = fmt.Errorf("%sNot able to get merged un-tracked branches: %s\n", retErr, err)
					continue
				} else {
					for _, mb := range mbs {
						mergedBranches[mb] = true
					}
				}
			}
			if !mergedBranches[b.Name] {
				continue
			}
			deleteForced = true
		}

		if b.Name == state.CurrentBranch.Name {
			untracked, err := g.HasUntrackedFiles()
			if err != nil {
				retErr = fmt.Errorf("%sNot deleting current branch %q as can't get changes: %s\n", retErr, b.Name, err)
				continue
			}
			uncommited, err := g.HasUncommittedChanges()
			if err != nil {
				retErr = fmt.Errorf("%sNot deleting current branch %q as can't get changes: %s\n", retErr, b.Name, err)
				continue
			}
			if untracked || uncommited {
				jirix.Logger.Debugf("Not deleting current branch %q for project %s(%s) as it has changes\n\n", b.Name, state.Project.Name, relativePath)
				continue
			}
			revision, err := project.GetHeadRevision(jirix, remote)
			if err != nil {
				retErr = fmt.Errorf("%sNot deleting current branch %q as can't get head revision: %s\n", retErr, b.Name, err)
				continue
			}
			if err := scm.CheckoutBranch(revision, gitutil.DetachOpt(true)); err != nil {
				retErr = fmt.Errorf("%sNot deleting current branch %q as can't checkout JIRI_HEAD: %s\n", retErr, b.Name, err)
				continue
			}
		}

		if err := scm.DeleteBranch(b.Name, gitutil.ForceOpt(deleteForced)); err != nil {
			if deleteForced {
				retErr = fmt.Errorf("%sCannot delete branch %q: %s\n", retErr, b.Name, err)
			}
			if b.Name == state.CurrentBranch.Name {
				if err := scm.CheckoutBranch(b.Name); err != nil {
					retErr = fmt.Errorf("%sNot able to put project back on branch %q: %s\n", retErr, b.Name, err)
				}
			}
			continue
		}
		deletedBranches = append(deletedBranches, b.Name)
	}
	return deletedBranches, retErr
}

func deleteBranches(jirix *jiri.X, branchToDelete string) error {
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	cDir, err := os.Getwd()
	if err != nil {
		return err
	}
	jirix.TimerPush("Get states")
	states, err := project.GetProjectStates(jirix, localProjects, false)
	if err != nil {
		return err
	}

	jirix.TimerPop()
	jirix.TimerPush("Process")
	errors := false
	projectFound := false
	var keys project.ProjectKeys
	for key, _ := range states {
		keys = append(keys, key)
	}
	sort.Sort(keys)
	for _, key := range keys {
		state := states[key]
		for _, branch := range state.Branches {
			if branch.Name == branchToDelete {
				projectFound = true
				localProject := state.Project
				relativePath, err := filepath.Rel(cDir, localProject.Path)
				if err != nil {
					return err
				}
				fmt.Printf("Project %s(%s): ", localProject.Name, relativePath)
				scm := gitutil.New(jirix, gitutil.RootDirOpt(localProject.Path))

				if err := scm.DeleteBranch(branchToDelete, gitutil.ForceOpt(branchFlags.forceDeleteFlag)); err != nil {
					errors = true
					fmt.Printf(jirix.Color.Red("Error while deleting branch: %s\n", err))
				} else {
					shortHash, err := scm.GetShortHash(branch.Revision)
					if err != nil {
						return err
					}
					fmt.Printf("%s (was %s)\n", jirix.Color.Green("Deleted Branch %s", branchToDelete), jirix.Color.Yellow(shortHash))
				}
				break
			}
		}
	}
	jirix.TimerPop()

	if !projectFound {
		fmt.Printf("Cannot find any project with branch %q\n", branchToDelete)
		return nil
	}
	if errors {
		fmt.Println(jirix.Color.Yellow("Please check errors above"))
	}
	return nil
}
