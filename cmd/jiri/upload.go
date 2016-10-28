// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"net/url"
	"strings"

	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/cmdline"
	"fuchsia.googlesource.com/jiri/gerrit"
	"fuchsia.googlesource.com/jiri/gitutil"
)

var (
	uploadCcsFlag          string
	uploadEditFlag         bool
	uploadHostFlag         string
	uploadPresubmitFlag    string
	uploadRemoteBranchFlag string
	uploadReviewersFlag    string
	uploadTopicFlag        string
	uploadVerifyFlag       bool
)

var cmdUpload = &cmdline.Command{
	Runner: jiri.RunnerFunc(runUpload),
	Name:   "upload",
	Short:  "Upload a changelist for review",
	Long:   `Command "upload" uploads all commits of a local branch to Gerrit.`,
}

func init() {
	cmdUpload.Flags.StringVar(&uploadCcsFlag, "cc", "", `Comma-separated list of emails or LDAPs to cc.`)
	cmdUpload.Flags.StringVar(&uploadHostFlag, "host", "", `Gerrit host to use.  Defaults to gerrit host specified in manifest.`)
	cmdUpload.Flags.StringVar(&uploadPresubmitFlag, "presubmit", string(gerrit.PresubmitTestTypeAll),
		fmt.Sprintf("The type of presubmit tests to run. Valid values: %s.", strings.Join(gerrit.PresubmitTestTypes(), ",")))
	cmdUpload.Flags.StringVar(&uploadRemoteBranchFlag, "remote-branch", "", `Name of the remote branch the CL pertains to, without the leading "origin/".`)
	cmdUpload.Flags.StringVar(&uploadReviewersFlag, "r", "", `Comma-separated list of emails or LDAPs to request review.`)
	cmdUpload.Flags.StringVar(&uploadTopicFlag, "topic", "", `CL topic.`)
	cmdUpload.Flags.BoolVar(&uploadVerifyFlag, "verify", true, `Run pre-push git hooks.`)
}

// runUpload is a wrapper that sets up and runs a review instance across
// multiple projects.
func runUpload(jirix *jiri.X, _ []string) error {
	git := gitutil.New(jirix.NewSeq())
	remoteBranch := "master"
	if uploadRemoteBranchFlag != "" {
		remoteBranch = uploadRemoteBranchFlag
	} else {
		if git.IsOnBranch() {
			trackingBranch, err := git.TrackingBranchName()
			if err != nil {
				return err
			}
			if trackingBranch != "" {
				// sometimes if user creates a local branch origin/branch
				// then remote branch is represented as remotes/origin/branch
				originIndex := strings.Index(trackingBranch, "origin/")
				if originIndex != -1 {
					trackingBranch = trackingBranch[originIndex+len("origin/"):]
				}
				remoteBranch = trackingBranch
			}
		}
	}
	p, err := currentProject(jirix)
	if err != nil {
		return err
	}

	host := uploadHostFlag
	if host == "" {
		if p.GerritHost == "" {
			return fmt.Errorf("No gerrit host found.  Please use the '--host' flag, or add a 'gerrithost' attribute for project %q.", p.Name)
		}
		host = p.GerritHost
	}
	hostUrl, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("invalid Gerrit host %q: %v", host, err)
	}
	projectRemoteUrl, err := url.Parse(p.Remote)
	if err != nil {
		return fmt.Errorf("invalid project remote: %v", p.Remote, err)
	}
	gerritRemote := *hostUrl
	gerritRemote.Path = projectRemoteUrl.Path
	opts := gerrit.CLOpts{
		Ccs:          parseEmails(uploadCcsFlag),
		Edit:         uploadEditFlag,
		Host:         hostUrl,
		Presubmit:    gerrit.PresubmitTestType(uploadPresubmitFlag),
		RemoteBranch: remoteBranch,
		Remote:       gerritRemote.String(),
		Reviewers:    parseEmails(uploadReviewersFlag),
		Verify:       uploadVerifyFlag,
	}
	branch, err := gitutil.New(jirix.NewSeq()).CurrentBranchName()
	if err != nil {
		return err
	}
	opts.Branch = branch

	if opts.Presubmit == gerrit.PresubmitTestType("") {
		opts.Presubmit = gerrit.PresubmitTestTypeAll // use gerrit.PresubmitTestTypeAll as the default
	}
	if err := gerrit.Push(jirix.NewSeq(), opts); err != nil {
		return gerritError(err.Error())
	}
	return nil
}
