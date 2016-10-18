// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/gerrit"
	"fuchsia.googlesource.com/jiri/gitutil"
	"fuchsia.googlesource.com/jiri/jiritest"
	"fuchsia.googlesource.com/jiri/project"
	"fuchsia.googlesource.com/jiri/runutil"
)

// assertCommitCount asserts that the commit count between two
// branches matches the expectedCount.
func assertCommitCount(t *testing.T, jirix *jiri.X, branch, baseBranch string, expectedCount int) {
	got, err := gitutil.New(jirix.NewSeq()).CountCommits(branch, baseBranch)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if want := 1; got != want {
		t.Fatalf("unexpected number of commits: got %v, want %v", got, want)
	}
}

// assertFileContent asserts that the content of the given file
// matches the expected content.
func assertFileContent(t *testing.T, jirix *jiri.X, file, want string) {
	got, err := jirix.NewSeq().ReadFile(file)
	if err != nil {
		t.Fatalf("%v\n", err)
	}
	if string(got) != want {
		t.Fatalf("unexpected content of file %v: got %v, want %v", file, got, want)
	}
}

// assertFilesExist asserts that the files exist.
func assertFilesExist(t *testing.T, jirix *jiri.X, files []string) {
	s := jirix.NewSeq()
	for _, file := range files {
		if _, err := s.Stat(file); err != nil {
			if runutil.IsNotExist(err) {
				t.Fatalf("expected file %v to exist but it did not", file)
			}
			t.Fatalf("%v", err)
		}
	}
}

// assertFilesDoNotExist asserts that the files do not exist.
func assertFilesDoNotExist(t *testing.T, jirix *jiri.X, files []string) {
	s := jirix.NewSeq()
	for _, file := range files {
		if _, err := s.Stat(file); err != nil && !runutil.IsNotExist(err) {
			t.Fatalf("%v", err)
		} else if err == nil {
			t.Fatalf("expected file %v to not exist but it did", file)
		}
	}
}

// assertFilesCommitted asserts that the files exist and are committed
// in the current branch.
func assertFilesCommitted(t *testing.T, jirix *jiri.X, files []string) {
	assertFilesExist(t, jirix, files)
	for _, file := range files {
		if !gitutil.New(jirix.NewSeq()).IsFileCommitted(file) {
			t.Fatalf("expected file %v to be committed but it is not", file)
		}
	}
}

// assertFilesNotCommitted asserts that the files exist and are *not*
// committed in the current branch.
func assertFilesNotCommitted(t *testing.T, jirix *jiri.X, files []string) {
	assertFilesExist(t, jirix, files)
	for _, file := range files {
		if gitutil.New(jirix.NewSeq()).IsFileCommitted(file) {
			t.Fatalf("expected file %v not to be committed but it is", file)
		}
	}
}

// assertFilesPushedToRef asserts that the given files have been
// pushed to the given remote repository reference.
func assertFilesPushedToRef(t *testing.T, jirix *jiri.X, repoPath, gerritPath, pushedRef string, files []string) {
	chdir(t, jirix, gerritPath)
	assertCommitCount(t, jirix, pushedRef, "master", 1)
	if err := gitutil.New(jirix.NewSeq()).CheckoutBranch(pushedRef); err != nil {
		t.Fatalf("%v", err)
	}
	assertFilesCommitted(t, jirix, files)
	chdir(t, jirix, repoPath)
}

// assertStashSize asserts that the stash size matches the expected
// size.
func assertStashSize(t *testing.T, jirix *jiri.X, want int) {
	got, err := gitutil.New(jirix.NewSeq()).StashSize()
	if err != nil {
		t.Fatalf("%v", err)
	}
	if got != want {
		t.Fatalf("unxpected stash size: got %v, want %v", got, want)
	}
}

// commitFile commits a file with the specified content into a branch
func commitFile(t *testing.T, jirix *jiri.X, filename string, content string) {
	s := jirix.NewSeq()
	if err := s.WriteFile(filename, []byte(content), 0644).Done(); err != nil {
		t.Fatalf("%v", err)
	}
	commitMessage := "Commit " + filename
	if err := gitutil.New(jirix.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com")).CommitFile(filename, commitMessage); err != nil {
		t.Fatalf("%v", err)
	}
}

// commitFiles commits the given files into to current branch.
func commitFiles(t *testing.T, jirix *jiri.X, filenames []string) {
	// Create and commit the files one at a time.
	for _, filename := range filenames {
		content := "This is file " + filename
		commitFile(t, jirix, filename, content)
	}
}

// createRepo creates a new repository with the given prefix.
func createRepo(t *testing.T, jirix *jiri.X, prefix string) string {
	s := jirix.NewSeq()
	repoPath, err := s.TempDir(jirix.Root, "repo-"+prefix)
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	if err := os.Chmod(repoPath, 0777); err != nil {
		t.Fatalf("Chmod(%v) failed: %v", repoPath, err)
	}
	if err := gitutil.New(jirix.NewSeq()).Init(repoPath); err != nil {
		t.Fatalf("%v", err)
	}
	if err := s.MkdirAll(filepath.Join(repoPath, jiri.ProjectMetaDir), os.FileMode(0755)).Done(); err != nil {
		t.Fatalf("%v", err)
	}
	return repoPath
}

// Simple commit-msg hook that removes any existing Change-Id and adds a
// fake one.
var commitMsgHook string = `#!/bin/sh
MSG="$1"
cat $MSG | sed -e "/Change-Id/d" > $MSG.tmp
echo "Change-Id: I0000000000000000000000000000000000000000" >> $MSG.tmp
mv $MSG.tmp $MSG
`

// installCommitMsgHook links the gerrit commit-msg hook into a different repo.
func installCommitMsgHook(t *testing.T, jirix *jiri.X, repoPath string) {
	hookLocation := path.Join(repoPath, ".git/hooks/commit-msg")
	if err := jirix.NewSeq().WriteFile(hookLocation, []byte(commitMsgHook), 0755).Done(); err != nil {
		t.Fatalf("WriteFile(%v) failed: %v", hookLocation, err)
	}
}

// chdir changes the runtime working directory and traps any errors.
func chdir(t *testing.T, jirix *jiri.X, path string) {
	if err := jirix.NewSeq().Chdir(path).Done(); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Fatalf("%s: %d: Chdir(%v) failed: %v", file, line, path, err)
	}
}

// createRepoFromOrigin creates a Git repo tracking origin/master.
func createRepoFromOrigin(t *testing.T, jirix *jiri.X, subpath string, originPath string) string {
	repoPath := createRepo(t, jirix, subpath)
	chdir(t, jirix, repoPath)
	git := gitutil.New(jirix.NewSeq())
	if err := git.AddRemote("origin", originPath); err != nil {
		t.Fatalf("%v", err)
	}
	if err := git.Pull("origin", "master"); err != nil {
		t.Fatalf("%v", err)
	}
	return repoPath
}

// createTestRepos sets up three local repositories: origin, gerrit,
// and the main test repository which pulls from origin and can push
// to gerrit.
func createTestRepos(t *testing.T, jirix *jiri.X) (string, string, string) {
	// Create origin.
	originPath := createRepo(t, jirix, "origin")
	chdir(t, jirix, originPath)
	if err := gitutil.New(jirix.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com")).CommitWithMessage("initial commit"); err != nil {
		t.Fatalf("%v", err)
	}
	// Create test repo.
	repoPath := createRepoFromOrigin(t, jirix, "test", originPath)
	// Add Gerrit remote.
	gerritPath := createRepoFromOrigin(t, jirix, "gerrit", originPath)
	// Switch back to test repo.
	chdir(t, jirix, repoPath)
	return repoPath, originPath, gerritPath
}

// submit mocks a Gerrit review submit by pushing the Gerrit remote to origin.
// Actually origin pulls from Gerrit since origin isn't actually a bare git repo.
// Some of our tests actually rely on accessing .git in origin, so it must be non-bare.
func submit(t *testing.T, jirix *jiri.X, originPath string, gerritPath string, review *review) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	chdir(t, jirix, originPath)
	expectedRef := gerrit.Reference(review.CLOpts)
	if err := gitutil.New(jirix.NewSeq()).Pull(gerritPath, expectedRef); err != nil {
		t.Fatalf("Pull gerrit to origin failed: %v", err)
	}
	chdir(t, jirix, cwd)
}

// setupTest creates a setup for testing the review tool.
func setupTest(t *testing.T, installHook bool) (fake *jiritest.FakeJiriRoot, repoPath, originPath, gerritPath string, cleanup func()) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() failed: %v", err)
	}
	var cleanupFake func()
	if fake, cleanupFake = jiritest.NewFakeJiriRoot(t); err != nil {
		t.Fatalf("%v", err)
	}
	repoPath, originPath, gerritPath = createTestRepos(t, fake.X)
	if installHook == true {
		for _, path := range []string{repoPath, originPath, gerritPath} {
			installCommitMsgHook(t, fake.X, path)
		}
	}
	chdir(t, fake.X, repoPath)
	cleanup = func() {
		chdir(t, fake.X, oldWD)
		cleanupFake()
	}
	return
}

func createCLWithFiles(t *testing.T, jirix *jiri.X, branch string, files ...string) {
	if err := newCL(jirix, []string{branch}); err != nil {
		t.Fatalf("%v", err)
	}
	commitFiles(t, jirix, files)
}

// TestCleanupClean checks that cleanup succeeds if the branch to be
// cleaned up has been merged with the master.
func TestCleanupClean(t *testing.T) {
	fake, repoPath, originPath, _, cleanup := setupTest(t, true)
	defer cleanup()
	branch := "my-branch"
	git := gitutil.New(fake.X.NewSeq())
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}
	commitFiles(t, fake.X, []string{"file1", "file2"})
	if err := git.CheckoutBranch("master"); err != nil {
		t.Fatalf("%v", err)
	}
	chdir(t, fake.X, originPath)
	commitFiles(t, fake.X, []string{"file1", "file2"})
	chdir(t, fake.X, repoPath)
	if err := cleanupCL(fake.X, []string{branch}); err != nil {
		t.Fatalf("cleanup() failed: %v", err)
	}
	if git.BranchExists(branch) {
		t.Fatalf("cleanup failed to remove the feature branch")
	}
}

// TestCleanupDirty checks that cleanup is a no-op if the branch to be
// cleaned up has unmerged changes.
func TestCleanupDirty(t *testing.T) {
	fake, _, _, _, cleanup := setupTest(t, true)
	defer cleanup()
	branch := "my-branch"
	git := gitutil.New(fake.X.NewSeq())
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}
	files := []string{"file1", "file2"}
	commitFiles(t, fake.X, files)
	if err := git.CheckoutBranch("master"); err != nil {
		t.Fatalf("%v", err)
	}
	if err := cleanupCL(fake.X, []string{branch}); err == nil {
		t.Fatalf("cleanup did not fail when it should")
	}
	if err := git.CheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}
	assertFilesCommitted(t, fake.X, files)
}

// TestCreateReviewBranch checks that the temporary review branch is
// created correctly.
func TestCreateReviewBranch(t *testing.T) {
	fake, _, _, _, cleanup := setupTest(t, true)
	defer cleanup()
	branch := "my-branch"
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}
	files := []string{"file1", "file2", "file3"}
	commitFiles(t, fake.X, files)
	review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{})
	if err != nil {
		t.Fatalf("%v", err)
	}
	if expected, got := branch+"-REVIEW", review.reviewBranch; expected != got {
		t.Fatalf("Unexpected review branch name: expected %v, got %v", expected, got)
	}
	commitMessage := "squashed commit"
	if err := review.createReviewBranch(git, commitMessage); err != nil {
		t.Fatalf("%v", err)
	}
	// Verify that the branch exists.
	if !git.BranchExists(review.reviewBranch) {
		t.Fatalf("review branch not found")
	}
	if err := git.CheckoutBranch(review.reviewBranch); err != nil {
		t.Fatalf("%v", err)
	}
	assertCommitCount(t, fake.X, review.reviewBranch, "master", 1)
	assertFilesCommitted(t, fake.X, files)
}

// TestCreateReviewBranchWithEmptyChange checks that running
// createReviewBranch() on a branch with no changes will result in an
// EmptyChangeError.
func TestCreateReviewBranchWithEmptyChange(t *testing.T) {
	fake, _, _, _, cleanup := setupTest(t, true)
	defer cleanup()
	branch := "my-branch"
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}
	review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{Remote: branch})
	if err != nil {
		t.Fatalf("%v", err)
	}
	commitMessage := "squashed commit"
	err = review.createReviewBranch(git, commitMessage)
	if err == nil {
		t.Fatalf("creating a review did not fail when it should")
	}
	if _, ok := err.(emptyChangeError); !ok {
		t.Fatalf("unexpected error type: %v", err)
	}
}

// TestSendReview checks the various options for sending a review.
func TestSendReview(t *testing.T) {
	fake, repoPath, _, gerritPath, cleanup := setupTest(t, true)
	defer cleanup()
	branch := "my-branch"
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}
	files := []string{"file1"}
	commitFiles(t, fake.X, files)
	{
		// Test with draft = false, no reviewiers, and no ccs.
		review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{Remote: gerritPath})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if err := review.send(); err != nil {
			t.Fatalf("failed to send a review: %v", err)
		}
		expectedRef := gerrit.Reference(review.CLOpts)
		assertFilesPushedToRef(t, fake.X, repoPath, gerritPath, expectedRef, files)
	}
	{
		// Test with draft = true, no reviewers, and no ccs.
		review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{
			Draft:  true,
			Remote: gerritPath,
		})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if err := review.send(); err != nil {
			t.Fatalf("failed to send a review: %v", err)
		}
		expectedRef := gerrit.Reference(review.CLOpts)
		assertFilesPushedToRef(t, fake.X, repoPath, gerritPath, expectedRef, files)
	}
	{
		// Test with draft = false, reviewers, and no ccs.
		review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{
			Remote:    gerritPath,
			Reviewers: parseEmails("reviewer1,reviewer2@example.org"),
		})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if err := review.send(); err != nil {
			t.Fatalf("failed to send a review: %v", err)
		}
		expectedRef := gerrit.Reference(review.CLOpts)
		assertFilesPushedToRef(t, fake.X, repoPath, gerritPath, expectedRef, files)
	}
	{
		// Test with draft = true, reviewers, and ccs.
		review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{
			Ccs:       parseEmails("cc1@example.org,cc2"),
			Draft:     true,
			Remote:    gerritPath,
			Reviewers: parseEmails("reviewer3@example.org,reviewer4"),
		})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if err := review.send(); err != nil {
			t.Fatalf("failed to send a review: %v", err)
		}
		expectedRef := gerrit.Reference(review.CLOpts)
		assertFilesPushedToRef(t, fake.X, repoPath, gerritPath, expectedRef, files)
	}
}

// TestSendReviewNoChangeID checks that review.send() correctly errors when
// not run with a commit hook that adds a Change-Id.
func TestSendReviewNoChangeID(t *testing.T) {
	// Pass 'false' to setup so it doesn't install the commit-msg hook.
	fake, _, _, gerritPath, cleanup := setupTest(t, false)
	defer cleanup()
	branch := "my-branch"
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}
	commitFiles(t, fake.X, []string{"file1"})
	review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{Remote: gerritPath})
	if err != nil {
		t.Fatalf("%v", err)
	}
	err = review.send()
	if err == nil {
		t.Fatalf("sending a review did not fail when it should")
	}
	if _, ok := err.(noChangeIDError); !ok {
		t.Fatalf("unexpected error type: %v", err)
	}
}

// TestEndToEnd checks the end-to-end functionality of the review tool.
func TestEndToEnd(t *testing.T) {
	fake, repoPath, _, gerritPath, cleanup := setupTest(t, true)
	defer cleanup()
	branch := "my-branch"
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}
	files := []string{"file1", "file2", "file3"}
	commitFiles(t, fake.X, files)
	review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{Remote: gerritPath})
	if err != nil {
		t.Fatalf("%v", err)
	}
	setTopicFlag = false
	if err := review.run(git); err != nil {
		t.Fatalf("run() failed: %v", err)
	}
	expectedRef := gerrit.Reference(review.CLOpts)
	assertFilesPushedToRef(t, fake.X, repoPath, gerritPath, expectedRef, files)
}

// TestLabelsInCommitMessage checks the labels are correctly processed
// for the commit message.
//
// HACK ALERT: This test runs the review.run() function multiple
// times. The function ends up pushing a commit to a fake "gerrit"
// repository created by the setupTest() function. For the real gerrit
// repository, it is possible to push to the refs/for/change reference
// multiple times, because it is a special reference that "maps"
// incoming commits to CL branches based on the commit message
// Change-Id. The fake "gerrit" repository does not implement this
// logic and thus the same reference cannot be pushed to multiple
// times. To overcome this obstacle, the test takes advantage of the
// fact that the reference name is a function of the reviewers and
// uses different reviewers for different review runs.
func TestLabelsInCommitMessage(t *testing.T) {
	fake, repoPath, _, gerritPath, cleanup := setupTest(t, true)
	defer cleanup()
	s := fake.X.NewSeq()
	branch := "my-branch"
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}

	// Test setting -presubmit=none and autosubmit.
	files := []string{"file1", "file2", "file3"}
	commitFiles(t, fake.X, files)
	review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{
		Autosubmit: true,
		Presubmit:  gerrit.PresubmitTestTypeNone,
		Remote:     gerritPath,
		Reviewers:  parseEmails("run1"),
	})
	if err != nil {
		t.Fatalf("%v", err)
	}
	setTopicFlag = false
	if err := review.run(git); err != nil {
		t.Fatalf("%v", err)
	}
	expectedRef := gerrit.Reference(review.CLOpts)
	assertFilesPushedToRef(t, fake.X, repoPath, gerritPath, expectedRef, files)
	// The last three lines of the gerrit commit message file should be:
	// AutoSubmit
	// PresubmitTest: none
	// Change-Id: ...
	file, err := getCommitMessageFileName(review.jirix, review.CLOpts.Branch)
	if err != nil {
		t.Fatalf("%v", err)
	}
	bytes, err := s.ReadFile(file)
	if err != nil {
		t.Fatalf("%v\n", err)
	}
	content := string(bytes)
	lines := strings.Split(content, "\n")
	// Make sure the Change-Id line is the last line.
	if got := lines[len(lines)-1]; !strings.HasPrefix(got, "Change-Id") {
		t.Fatalf("no Change-Id line found: %s", got)
	}
	// Make sure the "AutoSubmit" label exists.
	if autosubmitLabelRE.FindString(content) == "" {
		t.Fatalf("AutoSubmit label doesn't exist in the commit message: %s", content)
	}
	// Make sure the "PresubmitTest" label exists.
	if presubmitTestLabelRE.FindString(content) == "" {
		t.Fatalf("PresubmitTest label doesn't exist in the commit message: %s", content)
	}

	// Test setting -presubmit=all but keep autosubmit=true.
	review, err = newReview(fake.X, git, project.Project{}, gerrit.CLOpts{
		Autosubmit: true,
		Remote:     gerritPath,
		Reviewers:  parseEmails("run2"),
	})
	if err != nil {
		t.Fatalf("%v", err)
	}
	if err := review.run(git); err != nil {
		t.Fatalf("%v", err)
	}
	expectedRef = gerrit.Reference(review.CLOpts)
	assertFilesPushedToRef(t, fake.X, repoPath, gerritPath, expectedRef, files)
	bytes, err = s.ReadFile(file)
	if err != nil {
		t.Fatalf("%v\n", err)
	}
	content = string(bytes)
	// Make sure there is no PresubmitTest=none any more.
	match := presubmitTestLabelRE.FindString(content)
	if match != "" {
		t.Fatalf("want no presubmit label line, got: %s", match)
	}
	// Make sure the "AutoSubmit" label still exists.
	if autosubmitLabelRE.FindString(content) == "" {
		t.Fatalf("AutoSubmit label doesn't exist in the commit message: %s", content)
	}

	// Test setting autosubmit=false.
	review, err = newReview(fake.X, git, project.Project{}, gerrit.CLOpts{
		Remote:    gerritPath,
		Reviewers: parseEmails("run3"),
	})
	if err != nil {
		t.Fatalf("%v", err)
	}
	if err := review.run(git); err != nil {
		t.Fatalf("%v", err)
	}
	expectedRef = gerrit.Reference(review.CLOpts)
	assertFilesPushedToRef(t, fake.X, repoPath, gerritPath, expectedRef, files)
	bytes, err = s.ReadFile(file)
	if err != nil {
		t.Fatalf("%v\n", err)
	}
	content = string(bytes)
	// Make sure there is no AutoSubmit label any more.
	match = autosubmitLabelRE.FindString(content)
	if match != "" {
		t.Fatalf("want no AutoSubmit label line, got: %s", match)
	}
}

// TestDirtyBranch checks that the tool correctly handles unstaged and
// untracked changes in a working branch with stashed changes.
func TestDirtyBranch(t *testing.T) {
	fake, _, _, gerritPath, cleanup := setupTest(t, true)
	defer cleanup()
	s := fake.X.NewSeq()
	branch := "my-branch"
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}
	files := []string{"file1", "file2"}
	commitFiles(t, fake.X, files)
	assertStashSize(t, fake.X, 0)
	stashedFile, stashedFileContent := "stashed-file", "stashed-file content"
	if err := s.WriteFile(stashedFile, []byte(stashedFileContent), 0644).Done(); err != nil {
		t.Fatalf("WriteFile(%v, %v) failed: %v", stashedFile, stashedFileContent, err)
	}
	if err := git.Add(stashedFile); err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := git.Stash(); err != nil {
		t.Fatalf("%v", err)
	}
	assertStashSize(t, fake.X, 1)
	modifiedFile, modifiedFileContent := "file1", "modified-file content"
	if err := s.WriteFile(modifiedFile, []byte(modifiedFileContent), 0644).Done(); err != nil {
		t.Fatalf("WriteFile(%v, %v) failed: %v", modifiedFile, modifiedFileContent, err)
	}
	stagedFile, stagedFileContent := "file2", "staged-file content"
	if err := s.WriteFile(stagedFile, []byte(stagedFileContent), 0644).Done(); err != nil {
		t.Fatalf("WriteFile(%v, %v) failed: %v", stagedFile, stagedFileContent, err)
	}
	if err := git.Add(stagedFile); err != nil {
		t.Fatalf("%v", err)
	}
	untrackedFile, untrackedFileContent := "file3", "untracked-file content"
	if err := s.WriteFile(untrackedFile, []byte(untrackedFileContent), 0644).Done(); err != nil {
		t.Fatalf("WriteFile(%v, %v) failed: %v", untrackedFile, untrackedFileContent, err)
	}
	review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{Remote: gerritPath})
	if err != nil {
		t.Fatalf("%v", err)
	}
	setTopicFlag = false
	if err := review.run(git); err == nil {
		t.Fatalf("run() didn't fail when it should")
	}
	assertFilesNotCommitted(t, fake.X, []string{stagedFile})
	assertFilesNotCommitted(t, fake.X, []string{untrackedFile})
	assertFileContent(t, fake.X, modifiedFile, modifiedFileContent)
	assertFileContent(t, fake.X, stagedFile, stagedFileContent)
	assertFileContent(t, fake.X, untrackedFile, untrackedFileContent)
	// As of git 2.4.3 "git stash pop" fails if there are uncommitted
	// changes in the index. So we need to commit them first.
	if err := git.Commit(); err != nil {
		t.Fatalf("%v", err)
	}
	assertStashSize(t, fake.X, 1)
	if err := git.StashPop(); err != nil {
		t.Fatalf("%v", err)
	}
	assertStashSize(t, fake.X, 0)
	assertFilesNotCommitted(t, fake.X, []string{stashedFile})
	assertFileContent(t, fake.X, stashedFile, stashedFileContent)
}

// TestRunInSubdirectory checks that the command will succeed when run from
// within a subdirectory of a branch that does not exist on master branch, and
// will return the user to the subdirectory after completion.
func TestRunInSubdirectory(t *testing.T) {
	fake, repoPath, _, gerritPath, cleanup := setupTest(t, true)
	defer cleanup()
	s := fake.X.NewSeq()
	branch := "my-branch"
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatalf("%v", err)
	}
	subdir := "sub/directory"
	subdirPerms := os.FileMode(0744)
	if err := s.MkdirAll(subdir, subdirPerms).Done(); err != nil {
		t.Fatalf("MkdirAll(%v, %v) failed: %v", subdir, subdirPerms, err)
	}
	files := []string{path.Join(subdir, "file1")}
	commitFiles(t, fake.X, files)
	chdir(t, fake.X, subdir)
	review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{Remote: gerritPath})
	if err != nil {
		t.Fatalf("%v", err)
	}
	setTopicFlag = false
	if err := review.run(git); err != nil {
		t.Fatalf("run() failed: %v", err)
	}
	path := path.Join(repoPath, subdir)
	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%v) failed: %v", path, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("%v", err)
	}
	got, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("EvalSymlinks(%v) failed: %v", cwd, err)
	}
	if got != want {
		t.Fatalf("unexpected working directory: got %v, want %v", got, want)
	}
	expectedRef := gerrit.Reference(review.CLOpts)
	assertFilesPushedToRef(t, fake.X, repoPath, gerritPath, expectedRef, files)
}

// TestProcessLabels checks that the processLabels function works as expected.
func TestProcessLabels(t *testing.T) {
	fake, _, _, _, cleanup := setupTest(t, true)
	defer cleanup()
	testCases := []struct {
		autosubmit      bool
		presubmitType   gerrit.PresubmitTestType
		originalMessage string
		expectedMessage string
	}{
		{
			presubmitType:   gerrit.PresubmitTestTypeNone,
			originalMessage: "",
			expectedMessage: "PresubmitTest: none\n",
		},
		{
			autosubmit:      true,
			presubmitType:   gerrit.PresubmitTestTypeNone,
			originalMessage: "",
			expectedMessage: "AutoSubmit\nPresubmitTest: none\n",
		},
		{
			presubmitType:   gerrit.PresubmitTestTypeNone,
			originalMessage: "review message\n",
			expectedMessage: "review message\nPresubmitTest: none\n",
		},
		{
			autosubmit:      true,
			presubmitType:   gerrit.PresubmitTestTypeNone,
			originalMessage: "review message\n",
			expectedMessage: "review message\nAutoSubmit\nPresubmitTest: none\n",
		},
		{
			presubmitType: gerrit.PresubmitTestTypeNone,
			originalMessage: `review message

Change-Id: I0000000000000000000000000000000000000000`,
			expectedMessage: `review message

PresubmitTest: none
Change-Id: I0000000000000000000000000000000000000000`,
		},
		{
			autosubmit:    true,
			presubmitType: gerrit.PresubmitTestTypeNone,
			originalMessage: `review message

Change-Id: I0000000000000000000000000000000000000000`,
			expectedMessage: `review message

AutoSubmit
PresubmitTest: none
Change-Id: I0000000000000000000000000000000000000000`,
		},
		{
			presubmitType:   gerrit.PresubmitTestTypeAll,
			originalMessage: "",
			expectedMessage: "",
		},
		{
			presubmitType:   gerrit.PresubmitTestTypeAll,
			originalMessage: "review message\n",
			expectedMessage: "review message\n",
		},
		{
			presubmitType: gerrit.PresubmitTestTypeAll,
			originalMessage: `review message

Change-Id: I0000000000000000000000000000000000000000`,
			expectedMessage: `review message

Change-Id: I0000000000000000000000000000000000000000`,
		},
	}
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	for _, test := range testCases {
		review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{
			Autosubmit: test.autosubmit,
			Presubmit:  test.presubmitType,
		})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if got := review.processLabelsAndCommitFile(test.originalMessage); got != test.expectedMessage {
			t.Fatalf("want %s, got %s", test.expectedMessage, got)
		}
	}
}

// TestCLNew checks the operation of the "jiri cl new" command.
func TestCLNew(t *testing.T) {
	fake, _, _, _, cleanup := setupTest(t, true)
	defer cleanup()

	// Create some dependent CLs.
	if err := newCL(fake.X, []string{"feature1"}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := newCL(fake.X, []string{"feature2"}); err != nil {
		t.Fatalf("%v", err)
	}

	// Check that their dependency paths have been recorded correctly.
	testCases := []struct {
		branch string
		data   []byte
	}{
		{
			branch: "feature1",
			data:   []byte("master"),
		},
		{
			branch: "feature2",
			data:   []byte("master\nfeature1"),
		},
	}
	s := fake.X.NewSeq()
	for _, testCase := range testCases {
		file, err := getDependencyPathFileName(fake.X, testCase.branch)
		if err != nil {
			t.Fatalf("%v", err)
		}
		data, err := s.ReadFile(file)
		if err != nil {
			t.Fatalf("%v", err)
		}
		if bytes.Compare(data, testCase.data) != 0 {
			t.Fatalf("unexpected data:\ngot\n%v\nwant\n%v", string(data), string(testCase.data))
		}
	}
}

// TestDependentClsWithEditDelete exercises a previously observed failure case
// where if a CL edits a file and a dependent CL deletes it, jiri cl upload after
// the deletion failed with unrecoverable merge errors.
func TestDependentClsWithEditDelete(t *testing.T) {
	fake, repoPath, originPath, gerritPath, cleanup := setupTest(t, true)
	defer cleanup()
	chdir(t, fake.X, originPath)
	commitFiles(t, fake.X, []string{"A", "B"})
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))

	chdir(t, fake.X, repoPath)
	if err := syncCL(fake.X, git); err != nil {
		t.Fatalf("%v", err)
	}
	assertFilesExist(t, fake.X, []string{"A", "B"})

	createCLWithFiles(t, fake.X, "editme", "C")
	if err := fake.X.NewSeq().WriteFile("B", []byte("Will I dream?"), 0644).Done(); err != nil {
		t.Fatalf("%v", err)
	}
	if err := git.Add("B"); err != nil {
		t.Fatalf("%v", err)
	}
	if err := git.CommitWithMessage("editing stuff"); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}
	review, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{
		Remote:    gerritPath,
		Reviewers: parseEmails("run1"), // See hack note about TestLabelsInCommitMessage
	})
	if err != nil {
		t.Fatalf("%v", err)
	}
	setTopicFlag = false
	if err := review.run(git); err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	if err := newCL(fake.X, []string{"deleteme"}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := git.Remove("B", "C"); err != nil {
		t.Fatalf("git rm B C failed: %v", err)
	}
	if err := git.CommitWithMessage("deleting stuff"); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}
	review, err = newReview(fake.X, git, project.Project{}, gerrit.CLOpts{
		Remote:    gerritPath,
		Reviewers: parseEmails("run2"),
	})
	if err != nil {
		t.Fatalf("%v", err)
	}
	if err := review.run(git); err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	chdir(t, fake.X, gerritPath)
	expectedRef := gerrit.Reference(review.CLOpts)
	if err := gitutil.New(fake.X.NewSeq()).CheckoutBranch(expectedRef); err != nil {
		t.Fatalf("%v", err)
	}
	assertFilesExist(t, fake.X, []string{"A"})
	assertFilesDoNotExist(t, fake.X, []string{"B", "C"})
}

// TestParallelDev checks "jiri cl upload" behavior when parallel development has
// been submitted upstream.
func TestParallelDev(t *testing.T) {
	fake, repoPath, originPath, gerritAPath, cleanup := setupTest(t, true)
	defer cleanup()
	gerritBPath := createRepoFromOrigin(t, fake.X, "gerritB", originPath)
	chdir(t, fake.X, repoPath)

	// Create parallel branches with:
	// * non-conflicting changes in different files
	// * conflicting changes in a file
	createCLWithFiles(t, fake.X, "feature1-A", "A")

	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CheckoutBranch("master"); err != nil {
		t.Fatalf("%v", err)
	}
	createCLWithFiles(t, fake.X, "feature1-B", "B")
	commitFile(t, fake.X, "A", "Don't tread on me.")

	reviewB, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{Remote: gerritBPath})
	if err != nil {
		t.Fatalf("%v", err)
	}
	setTopicFlag = false
	if err := reviewB.run(git); err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	// Submit B and verify A doesn't revert it.
	submit(t, fake.X, originPath, gerritBPath, reviewB)

	// Assert files pushed to origin.
	chdir(t, fake.X, originPath)
	assertFilesExist(t, fake.X, []string{"A", "B"})
	chdir(t, fake.X, repoPath)

	if err := git.CheckoutBranch("feature1-A"); err != nil {
		t.Fatalf("%v", err)
	}

	reviewA, err := newReview(fake.X, git, project.Project{}, gerrit.CLOpts{Remote: gerritAPath})
	if err == nil {
		t.Fatalf("creating a review did not fail when it should")
	}
	// Assert state restored after failed review.
	assertFileContent(t, fake.X, "A", "This is file A")
	assertFilesDoNotExist(t, fake.X, []string{"B"})

	// Manual conflict resolution.
	if err := git.Merge("master", gitutil.ResetOnFailureOpt(false)); err == nil {
		t.Fatalf("merge applied cleanly when it shouldn't")
	}
	assertFilesNotCommitted(t, fake.X, []string{"A", "B"})
	assertFileContent(t, fake.X, "B", "This is file B")

	if err := fake.X.NewSeq().WriteFile("A", []byte("This is file A. Don't tread on me."), 0644).Done(); err != nil {
		t.Fatalf("%v", err)
	}

	if err := git.Add("A"); err != nil {
		t.Fatalf("%v", err)
	}
	if err := git.Add("B"); err != nil {
		t.Fatalf("%v", err)
	}
	if err := git.CommitWithMessage("Conflict resolution"); err != nil {
		t.Fatalf("%v", err)
	}

	// Retry review.
	reviewA, err = newReview(fake.X, git, project.Project{}, gerrit.CLOpts{Remote: gerritAPath})
	if err != nil {
		t.Fatalf("review failed: %v", err)
	}

	if err := reviewA.run(git); err != nil {
		t.Fatalf("run() failed: %v", err)
	}

	chdir(t, fake.X, gerritAPath)
	expectedRef := gerrit.Reference(reviewA.CLOpts)
	if err := git.CheckoutBranch(expectedRef); err != nil {
		t.Fatalf("%v", err)
	}
	assertFilesExist(t, fake.X, []string{"B"})
}

// TestCLSync checks the operation of the "jiri cl sync" command.
func TestCLSync(t *testing.T) {
	fake, _, _, _, cleanup := setupTest(t, true)
	defer cleanup()
	git := gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))

	// Create some dependent CLs.
	if err := newCL(fake.X, []string{"feature1"}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := newCL(fake.X, []string{"feature2"}); err != nil {
		t.Fatalf("%v", err)
	}

	// Add the "test" file to the master.
	if err := git.CheckoutBranch("master"); err != nil {
		t.Fatalf("%v", err)
	}
	commitFiles(t, fake.X, []string{"test"})

	// Sync the dependent CLs.
	if err := git.CheckoutBranch("feature2"); err != nil {
		t.Fatalf("%v", err)
	}
	if err := syncCL(fake.X, git); err != nil {
		t.Fatalf("%v", err)
	}

	// Check that the "test" file exists in the dependent CLs.
	for _, branch := range []string{"feature1", "feature2"} {
		if err := git.CheckoutBranch(branch); err != nil {
			t.Fatalf("%v", err)
		}
		assertFilesExist(t, fake.X, []string{"test"})
	}
}

func TestMultiPart(t *testing.T) {
	fake, cleanup := jiritest.NewFakeJiriRoot(t)
	defer cleanup()
	projects := addProjects(t, fake)

	origCleanupFlag, origCurrentProjectFlag := cleanupMultiPartFlag, currentProjectFlag
	defer func() {
		cleanupMultiPartFlag, currentProjectFlag = origCleanupFlag, origCurrentProjectFlag
	}()
	cleanupMultiPartFlag, currentProjectFlag = false, false

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	initMP := func() *multiPart {
		mp, err := initForMultiPart(fake.X)
		if err != nil {
			_, file, line, _ := runtime.Caller(1)
			t.Fatalf("%s:%d: %v", filepath.Base(file), line, err)
		}
		return mp
	}

	// A no-op function to return the given mp.  Needed for the `got, want := fn(), other_fn()` pattern.
	wr := func(mp *multiPart) *multiPart {
		return mp
	}

	git := func(dir string) *gitutil.Git {
		return gitutil.New(fake.X.NewSeq(), gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"), gitutil.RootDirOpt(dir))
	}

	// Paths that contain the various test projects -- many functions in `jiri cl` depend on the current
	// working directory (e.g. cl.go:projectStates), so when testing we must change to those directories.
	ra := projects[0].Path
	rb := projects[1].Path
	rc := projects[2].Path
	t1 := projects[3].Path

	relchdir := func(dir string) {
		chdir(t, fake.X, dir)
	}

	// This test checks whether the clean attribute is set when the cleanupMultiPartFlag is passed as true.
	git(ra).CreateAndCheckoutBranch("a1")
	relchdir(ra)
	cleanupMultiPartFlag = true
	got := initMP()
	want := &multiPart{clean: true, states: got.states, keys: got.keys}  // Don't care about the states/keys in this test, just pass them through.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}

	// This tests whether the current attribute is set when the currentProjectFlag is passed as true.
	currentProjectFlag = true
	if got, want := initMP(), wr(&multiPart{clean: true, current: true}); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
	cleanupMultiPartFlag, currentProjectFlag = false, false

	// Test metadata generation.
	git(ra).CreateAndCheckoutBranch("a1")
	relchdir(ra)

	if got, want := initMP(), wr(&multiPart{current: true, currentKey: projects[0].Key(), currentBranch: "a1"}); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}

	git(rb).CreateAndCheckoutBranch("a1")
	mp := initMP()
	if mp.current != false || mp.clean != false {
		t.Errorf("current or clean not false: %v, %v", mp.current, mp.clean)
	}
	if got, want := len(mp.keys), 2; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	tmp := &multiPart{
		keys: project.ProjectKeys{projects[0].Key(), projects[1].Key()},
	}
	for i, k := range mp.keys {
		if got, want := k, tmp.keys[i]; got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	}
	if got, want := len(mp.states), 2; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	git(rc).CreateAndCheckoutBranch("a1")
	git(t1).CreateAndCheckoutBranch("a2")
	mp = initMP()
	if got, want := len(mp.keys), 3; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if err := mp.writeMultiPartMetadata(fake.X); err != nil {
		t.Fatal(err)
	}

	hasMetaData := func(total int, branch string, projectPaths ...string) {
		_, file, line, _ := runtime.Caller(1)
		loc := fmt.Sprintf("%s:%d", filepath.Base(file), line)
		for i, dir := range projectPaths {
			filename := filepath.Join(dir, jiri.ProjectMetaDir, branch, multiPartMetaDataFileName)
			msg, err := ioutil.ReadFile(filename)
			if err != nil {
				t.Fatalf("%s: %v", loc, err)
			}
			if got, want := string(msg), fmt.Sprintf("MultiPart: %d/%d\n", i+1, total); got != want {
				t.Errorf("%v: got %v, want %v", dir, got, want)
			}
		}
	}

	hasNoMetaData := func(branch string, projectPaths ...string) {
		_, file, line, _ := runtime.Caller(1)
		loc := fmt.Sprintf("%s:%d", filepath.Base(file), line)
		for _, dir := range projectPaths {
			filename := filepath.Join(fake.X.Root, dir, jiri.ProjectMetaDir, branch, multiPartMetaDataFileName)
			_, err := os.Stat(filename)
			if !os.IsNotExist(err) {
				t.Fatalf("%s: %s should not exist", loc, filename)
			}
		}
	}

	newFile := func(dir, file string) {
		testfile := filepath.Join(dir, file)
		_, err := fake.X.NewSeq().Create(testfile)
		if err != nil {
			t.Errorf("failed to create %s: %v", testfile, err)
		}
	}

	hasMetaData(len(mp.keys), "a1", ra, rb, rc)
	hasNoMetaData(t1, "a2")
	if err := mp.cleanMultiPartMetadata(fake.X); err != nil {
		t.Fatal(err)
	}
	hasNoMetaData(ra, "a1", rb, rc, t1)

	// Test CL messages.

	for _, p := range projects {
		// Install commit hook so that Change-Id is written.
		installCommitMsgHook(t, fake.X, p.Path)

	}

	// Create a fake jiri root for the fake gerrit repos.
	gerritFake, gerritCleanup := jiritest.NewFakeJiriRoot(t)
	defer gerritCleanup()

	relchdir(ra)

	if err := mp.writeMultiPartMetadata(fake.X); err != nil {
		t.Fatal(err)
	}
	hasMetaData(len(mp.keys), "a1", ra, rb, rc)

	gitAddFiles := func(name string, repos ...string) {
		for _, dir := range repos {
			newFile(dir, name)
			if err := git(dir).Add(name); err != nil {
				t.Error(err)
			}
		}
	}

	gitCommit := func(msg string, repos ...string) {
		for _, dir := range repos {
			committer := git(dir).NewCommitter(false)
			if err := committer.Commit(msg); err != nil {
				t.Error(err)
			}
		}
	}

	gitAddFiles("new-file", ra, rb, rc)
	_, err = initForMultiPart(fake.X)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes:") {
		t.Fatalf("expected an error about uncommitted changes: got %v", err)
	}

	gitCommit("oh multipart test\n", ra, rb, rc)
	bodyMessage := "xyz\n\na simple message\n"
	metaDir := filepath.Join(fake.X.Root, jiri.RootMetaDir)
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatal(err)
	}
	messageFile := filepath.Join(metaDir, "message-body")
	if err := ioutil.WriteFile(messageFile, []byte(bodyMessage), 0666); err != nil {
		t.Fatal(err)
	}

	mp = initMP()
	setTopicFlag = false
	commitMessageBodyFlag = messageFile

	testCommitMsgs := func(branch string, cls ...*project.Project) {
		_, file, line, _ := runtime.Caller(1)
		loc := fmt.Sprintf("%s:%d", filepath.Base(file), line)

		total := len(cls)
		for index, p := range cls {
			// Create a new gerrit repo each time we commit, since we can't
			// push more than once to the fake gerrit repo without actually
			// running gerrit.
			gp := createRepoFromOrigin(t, gerritFake.X, "gerrit", p.Remote)
			defer os.Remove(gp)
			relchdir(p.Path)
			review, err := newReview(fake.X, git(p.Path), *p, gerrit.CLOpts{
				Presubmit: gerrit.PresubmitTestTypeNone,
				Remote:    gp,
			})
			if err != nil {
				t.Fatalf("%v: %v: %v", loc, p.Path, err)
			}
			// use the default commit message
			if err := review.run(git(p.Path)); err != nil {
				t.Fatalf("%v: %v, %v", loc, p.Path, err)
			}
			filename, err := getCommitMessageFileName(fake.X, branch)
			if err != nil {
				t.Fatalf("%v: %v", loc, err)
			}
			msg, err := ioutil.ReadFile(filename)
			if err != nil {
				t.Fatalf("%v: %v", loc, err)
			}
			if total < 2 {
				if strings.Contains(string(msg), "MultiPart") {
					t.Errorf("%v: commit message contains MultiPart when it should not: %v", loc, string(msg))
				}
				continue
			}
			expected := fmt.Sprintf("\nMultiPart: %d/%d\n", index+1, total)
			if !strings.Contains(string(msg), expected) {
				t.Errorf("%v: commit message for %v does not contain %v: %v", loc, p.Path, expected, string(msg))
			}
			if got, want := string(msg), bodyMessage+"PresubmitTest: none"+expected+"Change-Id: I0000000000000000000000000000000000000000"; got != want {
				t.Errorf("got %v, want %v", got, want)
			}
		}
	}

	testCommitMsgs("a1", projects[0], projects[1], projects[2])

	cl := mp.commandline("", []string{"-r=alice"})
	expected := []string{
		"runp",
		"--interactive",
		"--projects=" + string(projects[0].Key()) + "," + string(projects[1].Key()) + "," + string(projects[2].Key()),
		"jiri",
		"cl",
		"upload",
		"--current-project-only=true",
		"-r=alice",
	}
	if got, want := strings.Join(cl, " "), strings.Join(expected, " "); got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	cl = mp.commandline(projects[0].Key(), []string{"-r=bob"})
	expected[2] = "--projects=" + string(projects[1].Key()) + "," + string(projects[2].Key())
	expected[len(expected)-1] = "-r=bob"
	if got, want := strings.Join(cl, " "), strings.Join(expected, " "); got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	git(rb).CreateAndCheckoutBranch("a2")
	gitAddFiles("new-file1", ra, rc)
	gitCommit("oh multipart test: 2\n", ra, rc)

	mp = initMP()
	if err := mp.writeMultiPartMetadata(fake.X); err != nil {
		t.Fatal(err)
	}
	hasMetaData(len(mp.keys), "a1", ra, rc)
	testCommitMsgs("a1", projects[0], projects[2])

	git(ra).CreateAndCheckoutBranch("a2")

	mp = initMP()
	if err := mp.writeMultiPartMetadata(fake.X); err != nil {
		t.Fatal(err)
	}
	hasNoMetaData(rc)
	testCommitMsgs("a1", projects[2])
}
