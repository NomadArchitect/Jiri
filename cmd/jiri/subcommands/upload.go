// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/gerrit"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/project"
)

var uploadFlags struct {
	ccs          string
	presubmit    string
	reviewers    string
	topic        string
	verify       bool
	rebase       bool
	setTopic     bool
	multipart    bool
	branch       string
	remoteBranch string
	labels       string
	gitOptions   string
}

type uploadError string

func (e uploadError) Error() string {
	result := "sending code review failed\n\n"
	result += string(e)
	return result
}

var cmdUpload = &cmdline.Command{
	Runner:   jiri.RunnerFunc(runUpload),
	Name:     "upload",
	Short:    "Upload a changelist for review",
	Long:     `Command "upload" uploads commits of a local branch to Gerrit.`,
	ArgsName: "<ref>",
	ArgsLong: `
<ref> is the valid git ref to upload. It is optional and HEAD is used by
default. This cannot be used with -multipart flag.
`,
}

func init() {
	cmdUpload.Flags.StringVar(&uploadFlags.ccs, "cc", "", `Comma-separated list of emails or LDAPs to cc.`)
	cmdUpload.Flags.StringVar(&uploadFlags.presubmit, "presubmit", string(gerrit.PresubmitTestTypeAll),
		fmt.Sprintf("The type of presubmit tests to run. Valid values: %s.", strings.Join(gerrit.PresubmitTestTypes(), ",")))
	cmdUpload.Flags.StringVar(&uploadFlags.reviewers, "r", "", `Comma-separated list of emails or LDAPs to request review.`)
	cmdUpload.Flags.StringVar(&uploadFlags.labels, "l", "", `Comma-separated list of review labels.`)
	cmdUpload.Flags.StringVar(&uploadFlags.topic, "topic", "", `CL topic. Default is <username>-<branchname>. If this flag is set, upload will ignore -set-topic and will set a topic.`)
	cmdUpload.Flags.BoolVar(&uploadFlags.setTopic, "set-topic", false, `Set topic. This flag would be ignored if -topic passed.`)
	cmdUpload.Flags.BoolVar(&uploadFlags.verify, "verify", true, `Run pre-push git hooks.`)
	cmdUpload.Flags.BoolVar(&uploadFlags.rebase, "rebase", false, `Run rebase before pushing.`)
	cmdUpload.Flags.BoolVar(&uploadFlags.multipart, "multipart", false, `Send multipart CL.  Use -set-topic or -topic flag if you want to set a topic.`)
	cmdUpload.Flags.StringVar(&uploadFlags.branch, "branch", "", `Used when multipart flag is true and this command is executed from root folder`)
	cmdUpload.Flags.StringVar(&uploadFlags.remoteBranch, "remoteBranch", "", `Remote branch to upload change to. If this is not specified and branch is untracked,
change would be uploaded to branch in project manifest`)
	cmdUpload.Flags.StringVar(&uploadFlags.gitOptions, "git-options", "", `Passthrough git options`)
}

// runUpload is a wrapper that pushes the changes to gerrit for review.
func runUpload(jirix *jiri.X, args []string) error {
	refToUpload := "HEAD"
	if len(args) == 1 {
		refToUpload = args[0]
	} else if len(args) > 1 {
		return jirix.UsageErrorf("wrong number of arguments")
	}
	if uploadFlags.multipart && refToUpload != "HEAD" {
		return jirix.UsageErrorf("can only use HEAD as <ref> when using -multipart flag.")
	}
	cwd := jirix.Cwd
	var p *project.Project
	// Walk up the path until we find a project at that path, or hit the jirix.Root parent.
	// Note that we can't just compare path prefixes because of soft links.
	for cwd != filepath.Dir(jirix.Root) && cwd != string(filepath.Separator) {
		if isLocal, err := project.IsLocalProject(jirix, cwd); err != nil {
			return fmt.Errorf("Error while checking for local project at path %q: %s", cwd, err)
		} else if !isLocal {
			cwd = filepath.Dir(cwd)
			continue
		}
		project, err := project.ProjectAtPath(jirix, cwd)
		if err != nil {
			return fmt.Errorf("Error while getting project at path %q: %s", cwd, err)
		}
		p = &project
		break
	}

	setTopic := uploadFlags.setTopic

	// Always set topic when either topic is passed.
	if uploadFlags.topic != "" {
		setTopic = true
	}

	currentBranch := ""
	if p == nil {
		if !uploadFlags.multipart {
			return fmt.Errorf("directory %q is not contained in a project", cwd)
		} else if uploadFlags.branch == "" {
			return fmt.Errorf("Please run with -branch flag")
		}
		currentBranch = uploadFlags.branch
	} else {
		scm := gitutil.New(jirix, gitutil.RootDirOpt(p.Path))
		if !scm.IsOnBranch() {
			if uploadFlags.multipart {
				return fmt.Errorf("Current project is not on any branch. Multipart uploads require project to be on a branch.")
			}
			if uploadFlags.topic == "" && setTopic {
				return fmt.Errorf("Current project is not on any branch. Either provide a topic or set flag \"-set-topic\" to false.")
			}
		} else {
			var err error
			currentBranch, err = scm.CurrentBranchName()
			if err != nil {
				return err
			}
		}
	}
	var projectsToProcess []project.Project
	topic := ""
	if setTopic {
		if topic = uploadFlags.topic; topic == "" {
			topic = fmt.Sprintf("%s-%s", os.Getenv("USER"), currentBranch) // use <username>-<branchname> as the default
		}
	}
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	if uploadFlags.multipart {
		for _, project := range localProjects {
			scm := gitutil.New(jirix, gitutil.RootDirOpt(project.Path))
			if scm.IsOnBranch() {
				branch, err := scm.CurrentBranchName()
				if err != nil {
					return err
				}
				if currentBranch == branch {
					projectsToProcess = append(projectsToProcess, project)
				}
			}
		}

	} else {
		projectsToProcess = append(projectsToProcess, *p)
	}
	if len(projectsToProcess) == 0 {
		return fmt.Errorf("Did not find any project to push for branch %q", currentBranch)
	}
	type GerritPushOption struct {
		Project      project.Project
		CLOpts       gerrit.CLOpts
		relativePath string
	}
	var gerritPushOptions []GerritPushOption
	remoteProjects, _, _, err := project.LoadManifestFile(jirix, jirix.JiriManifestFile(), localProjects, false /*localManifest*/)
	if err != nil {
		return err
	}
	for _, project := range projectsToProcess {
		scm := gitutil.New(jirix, gitutil.RootDirOpt(project.Path))
		relativePath, err := filepath.Rel(cwd, project.Path)
		if err != nil {
			// Just use the full path if an error occurred.
			relativePath = project.Path
		}
		if uploadFlags.rebase {
			if changes, err := gitutil.New(jirix, gitutil.RootDirOpt(project.Path)).HasUncommittedChanges(); err != nil {
				return err
			} else if changes {
				return fmt.Errorf("Project %s(%s) has uncommitted changes, please commit them or stash them. Cannot rebase before pushing.", project.Name, relativePath)
			}
		}
		remoteBranch := uploadFlags.remoteBranch
		if remoteBranch == "" && currentBranch != "" {
			remoteBranch, err = scm.RemoteBranchName()
			if err != nil {
				return err
			}
		}
		if remoteBranch == "" { // Un-tracked branch
			remoteBranch = "main"
			if r, ok := remoteProjects[project.Key()]; ok {
				remoteBranch = r.RemoteBranch
			} else {
				jirix.Logger.Warningf("Project %s(%s) not found in manifest, will upload change to %q", project.Name, relativePath, remoteBranch)
			}
		}

		opts := gerrit.CLOpts{
			Ccs:          parseEmails(uploadFlags.ccs),
			GitOptions:   uploadFlags.gitOptions,
			Presubmit:    gerrit.PresubmitTestType(uploadFlags.presubmit),
			RemoteBranch: remoteBranch,
			Remote:       "origin",
			Reviewers:    parseEmails(uploadFlags.reviewers),
			Labels:       parseLabels(uploadFlags.labels),
			Verify:       uploadFlags.verify,
			Topic:        topic,
			RefToUpload:  refToUpload,
		}

		if opts.Presubmit == gerrit.PresubmitTestType("") {
			opts.Presubmit = gerrit.PresubmitTestTypeAll
		}
		gerritPushOptions = append(gerritPushOptions, GerritPushOption{project, opts, relativePath})
	}

	// Rebase all projects before pushing
	if uploadFlags.rebase {
		for _, gerritPushOption := range gerritPushOptions {
			scm := gitutil.New(jirix, gitutil.RootDirOpt(gerritPushOption.Project.Path))
			if err := scm.Fetch("origin", jirix.EnableSubmodules); err != nil {
				return err
			}
			remoteBranch := "remotes/origin/" + gerritPushOption.CLOpts.RemoteBranch
			if err = scm.Rebase(remoteBranch); err != nil {
				if err2 := scm.RebaseAbort(); err2 != nil {
					return err2
				}
				return fmt.Errorf("For project %s(%s), not able to rebase the branch to %s, please rebase manually: %s", gerritPushOption.Project.Name, gerritPushOption.relativePath, remoteBranch, err)
			}
		}
	}

	for _, gerritPushOption := range gerritPushOptions {
		fmt.Printf("Pushing project %s(%s)\n", gerritPushOption.Project.Name, gerritPushOption.relativePath)
		if err := gerrit.Push(jirix, gerritPushOption.Project.Path, gerritPushOption.CLOpts); err != nil {
			if strings.Contains(err.Error(), "(no new changes)") {
				if gitErr, ok := err.(gerrit.PushError); ok {
					fmt.Printf("%s", gitErr.Output)
					fmt.Printf("%s", gitErr.ErrorOutput)
				} else {
					return uploadError(err.Error())
				}
			} else {
				return uploadError(err.Error())
			}
		}
		fmt.Println()
	}
	return nil
}

// parseEmails input a list of comma separated tokens and outputs a
// list of email addresses. The tokens can either be email addresses
// or Google LDAPs in which case the suffix @google.com is appended to
// them to turn them into email addresses.
func parseEmails(value string) []string {
	var emails []string
	tokens := strings.Split(value, ",")
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if !strings.Contains(token, "@") {
			token += "@google.com"
		}
		emails = append(emails, token)
	}
	return emails
}

// parseLabels input a list of comma separated tokens and outputs a
// list of tokens without whitespaces
func parseLabels(value string) []string {
	var ret []string
	tokens := strings.Split(value, ",")
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		ret = append(ret, token)
	}
	return ret
}
