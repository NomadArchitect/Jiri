// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/git"
	"fuchsia.googlesource.com/jiri/gitutil"
	"fuchsia.googlesource.com/jiri/jiritest"
	"fuchsia.googlesource.com/jiri/project"
)

func setDefaultStatusFlags() {
	statusFlags.changes = true
	statusFlags.notHead = true
	statusFlags.branch = ""
	statusFlags.commits = true
}

func createCommits(t *testing.T, fake *jiritest.FakeJiriRoot, localProjects []project.Project) ([]string, []string, []string, []string) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var file2CommitRevs []string
	var file1CommitRevs []string
	var latestCommitRevs []string
	var relativePaths []string
	s := fake.X.NewSeq()
	for i, localProject := range localProjects {
		setDummyUser(t, fake.X, fake.Projects[localProject.Name])
		gr := git.NewGit(fake.Projects[localProject.Name])
		gitRemote := gitutil.New(s, gitutil.RootDirOpt(fake.Projects[localProject.Name]))
		writeFile(t, fake.X, fake.Projects[localProject.Name], "file1"+strconv.Itoa(i), "file1"+strconv.Itoa(i))
		gitRemote.CreateAndCheckoutBranch("file-1")
		gitRemote.CheckoutBranch("master")
		file1CommitRev, _ := gr.CurrentRevision()
		file1CommitRevs = append(file1CommitRevs, file1CommitRev)
		gitRemote.CreateAndCheckoutBranch("file-2")
		gitRemote.CheckoutBranch("master")
		writeFile(t, fake.X, fake.Projects[localProject.Name], "file2"+strconv.Itoa(i), "file2"+strconv.Itoa(i))
		file2CommitRev, _ := gr.CurrentRevision()
		file2CommitRevs = append(file2CommitRevs, file2CommitRev)
		writeFile(t, fake.X, fake.Projects[localProject.Name], "file3"+strconv.Itoa(i), "file3"+strconv.Itoa(i))
		file3CommitRev, _ := gr.CurrentRevision()
		latestCommitRevs = append(latestCommitRevs, file3CommitRev)
		relativePath, _ := filepath.Rel(cwd, localProject.Path)
		relativePaths = append(relativePaths, relativePath)
	}
	return file1CommitRevs, file2CommitRevs, latestCommitRevs, relativePaths
}

func createProjects(t *testing.T, fake *jiritest.FakeJiriRoot, numProjects int) []project.Project {
	localProjects := []project.Project{}
	for i := 0; i < numProjects; i++ {
		name := fmt.Sprintf("project-%d", i)
		path := fmt.Sprintf("path-%d", i)
		if err := fake.CreateRemoteProject(name); err != nil {
			t.Fatal(err)
		}
		p := project.Project{
			Name:   name,
			Path:   filepath.Join(fake.X.Root, path),
			Remote: fake.Projects[name],
		}
		localProjects = append(localProjects, p)
		if err := fake.AddProject(p); err != nil {
			t.Fatal(err)
		}
	}
	return localProjects
}

func expectedOutput(t *testing.T, fake *jiritest.FakeJiriRoot, localProjects []project.Project,
	latestCommitRevs, currentCommits, changes, currentBranch, relativePaths []string, extraCommitLogs [][]string) string {
	want := ""
	for i, localProject := range localProjects {
		includeForNotHead := statusFlags.notHead && currentCommits[i] != latestCommitRevs[i]
		includeForChanges := statusFlags.changes && changes[i] != ""
		includeProject := (statusFlags.branch == "" && (includeForNotHead || includeForChanges)) ||
			(statusFlags.branch != "" && statusFlags.branch == currentBranch[i])
		if includeProject {
			want = fmt.Sprintf("%v%v(%v): ", want, localProject.Name, relativePaths[i])
			if currentCommits[i] != latestCommitRevs[i] && statusFlags.notHead {
				want = fmt.Sprintf("%vShould be on revision %q, but is on revision %q", want, latestCommitRevs[i], currentCommits[i])
			}
			want = fmt.Sprintf("%v\nBranch: ", want)
			branchmsg := currentBranch[i]
			if branchmsg == "" {
				branchmsg = fmt.Sprintf("DETACHED-HEAD(%v)", currentCommits[i])
			}
			want = fmt.Sprintf("%v%v", want, branchmsg)
			if statusFlags.branch != "" && statusFlags.commits && len(extraCommitLogs[i]) != 0 {
				want = fmt.Sprintf("%v\nCommits: %v commit(s) not merged to remote", want, len(extraCommitLogs[i]))
				for _, commitLog := range extraCommitLogs[i] {
					want = fmt.Sprintf("%v\n%v", want, commitLog)
				}

			}
			if statusFlags.changes && changes[i] != "" {
				want = fmt.Sprintf("%v\n%v", want, changes[i])
			}
			want = fmt.Sprintf("%v\n\n", want)
		}
	}
	want = strings.TrimSpace(want)
	return want
}

func TestStatus(t *testing.T) {
	setDefaultStatusFlags()
	fake, cleanup := jiritest.NewFakeJiriRoot(t)
	defer cleanup()
	s := fake.X.NewSeq()

	// Add projects
	numProjects := 3
	localProjects := createProjects(t, fake, numProjects)
	file1CommitRevs, file2CommitRevs, latestCommitRevs, relativePaths := createCommits(t, fake, localProjects)
	if err := fake.UpdateUniverse(false); err != nil {
		t.Fatal(err)
	}

	for _, lp := range localProjects {
		setDummyUser(t, fake.X, lp.Path)
	}
	// Test no changes
	got := executeStatus(t, fake, "")
	want := ""
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	// Test when HEAD is on different revsion
	gitLocal := gitutil.New(s, gitutil.RootDirOpt(localProjects[1].Path))
	gitLocal.CheckoutBranch("HEAD~1")
	gitLocal = gitutil.New(s, gitutil.RootDirOpt(localProjects[2].Path))
	gitLocal.CheckoutBranch("file-2")
	got = executeStatus(t, fake, "")
	currentCommits := []string{latestCommitRevs[0], file2CommitRevs[1], file1CommitRevs[2]}
	currentBranch := []string{"", "", "file-2"}
	changes := []string{"", "", ""}
	want = expectedOutput(t, fake, localProjects, latestCommitRevs, currentCommits, changes, currentBranch, relativePaths, nil)
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	newfile := func(dir, file string) {
		testfile := filepath.Join(dir, file)
		_, err := s.Create(testfile)
		if err != nil {
			t.Errorf("failed to create %s: %v", testfile, err)
		}
	}

	// Test combinations of tracked and untracked changes
	newfile(localProjects[0].Path, "untracked1")
	newfile(localProjects[0].Path, "untracked2")
	newfile(localProjects[2].Path, "uncommitted.go")
	if err := gitLocal.Add("uncommitted.go"); err != nil {
		t.Error(err)
	}
	got = executeStatus(t, fake, "")
	currentCommits = []string{latestCommitRevs[0], file2CommitRevs[1], file1CommitRevs[2]}
	currentBranch = []string{"", "", "file-2"}
	changes = []string{"?? untracked1\n?? untracked2", "", "A  uncommitted.go"}
	want = expectedOutput(t, fake, localProjects, latestCommitRevs, currentCommits, changes, currentBranch, relativePaths, nil)
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func statusFlagsTest(t *testing.T) {
	fake, cleanup := jiritest.NewFakeJiriRoot(t)
	defer cleanup()
	s := fake.X.NewSeq()

	// Add projects
	numProjects := 6
	localProjects := createProjects(t, fake, numProjects)
	file1CommitRevs, file2CommitRevs, latestCommitRevs, relativePaths := createCommits(t, fake, localProjects)
	if err := fake.UpdateUniverse(false); err != nil {
		t.Fatal(err)
	}
	gitLocals := make([]*gitutil.Git, numProjects)
	for i, localProject := range localProjects {
		gitLocal := gitutil.New(s, gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"), gitutil.RootDirOpt(localProject.Path))
		gitLocals[i] = gitLocal
	}

	newfile := func(dir, file string) {
		testfile := filepath.Join(dir, file)
		_, err := s.Create(testfile)
		if err != nil {
			t.Errorf("failed to create %s: %v", testfile, err)
		}
	}

	gitLocals[0].CheckoutBranch("HEAD~1")
	gitLocals[1].CheckoutBranch("file-2")
	gitLocals[3].CheckoutBranch("HEAD~2")
	gitLocals[4].CheckoutBranch("master")
	gitLocals[5].CheckoutBranch("master")

	newfile(localProjects[0].Path, "untracked1")
	newfile(localProjects[0].Path, "untracked2")

	newfile(localProjects[1].Path, "uncommitted.go")
	if err := gitLocals[1].Add("uncommitted.go"); err != nil {
		t.Error(err)
	}

	newfile(localProjects[2].Path, "untracked1")
	newfile(localProjects[2].Path, "uncommitted.go")
	if err := gitLocals[2].Add("uncommitted.go"); err != nil {
		t.Error(err)
	}

	extraCommits5 := []string{}
	for i := 0; i < 2; i++ {
		file := fmt.Sprintf("extrafile%v", i)
		writeFile(t, fake.X, localProjects[5].Path, file, file+"log")
		log, err := gitLocals[5].OneLineLog("HEAD")
		if err != nil {
			t.Error(err)
		}
		extraCommits5 = append([]string{log}, extraCommits5...)
	}
	gl5 := git.NewGit(localProjects[5].Path)
	currentCommit5, err := gl5.CurrentRevision()
	if err != nil {
		t.Error(err)
	}
	got := executeStatus(t, fake, "")
	currentCommits := []string{file2CommitRevs[0], file1CommitRevs[1], latestCommitRevs[2], file1CommitRevs[3], latestCommitRevs[4], currentCommit5}
	extraCommitLogs := [][]string{nil, nil, nil, nil, nil, extraCommits5}
	currentBranch := []string{"", "file-2", "", "", "master", "master"}
	changes := []string{"?? untracked1\n?? untracked2", "A  uncommitted.go", "A  uncommitted.go\n?? untracked1", "", "", ""}
	want := expectedOutput(t, fake, localProjects, latestCommitRevs, currentCommits, changes, currentBranch, relativePaths, extraCommitLogs)
	if !equal(got, want) {
		printStatusFlags()
		t.Errorf("got %v, want %v", got, want)
	}
}

func printStatusFlags() {
	fmt.Printf("changes=%v, not-head=%v, commits=%v\n", statusFlags.changes, statusFlags.notHead, statusFlags.commits)
}

func TestStatusFlags(t *testing.T) {
	setDefaultStatusFlags()
	statusFlagsTest(t)

	setDefaultStatusFlags()
	statusFlags.changes = false
	statusFlagsTest(t)

	setDefaultStatusFlags()
	statusFlags.changes = false
	statusFlags.notHead = false
	statusFlagsTest(t)

	setDefaultStatusFlags()
	statusFlags.notHead = false
	statusFlagsTest(t)

	setDefaultStatusFlags()
	statusFlags.changes = false
	statusFlags.notHead = false
	statusFlags.branch = "master"
	statusFlagsTest(t)

	setDefaultStatusFlags()
	statusFlags.notHead = false
	statusFlags.branch = "master"
	statusFlags.commits = false
	statusFlagsTest(t)

	setDefaultStatusFlags()
	statusFlags.changes = false
	statusFlags.branch = "master"
	statusFlagsTest(t)

	setDefaultStatusFlags()
	statusFlags.changes = false
	statusFlags.notHead = false
	statusFlags.branch = "file-2"
	statusFlagsTest(t)

	setDefaultStatusFlags()
	statusFlags.notHead = false
	statusFlags.branch = "file-2"
	statusFlagsTest(t)

	setDefaultStatusFlags()
	statusFlags.changes = false
	statusFlags.branch = "file-2"
	statusFlagsTest(t)
}

func equal(first, second string) bool {
	firstStrings := strings.Split(first, "\n\n")
	secondStrings := strings.Split(second, "\n\n")
	if len(firstStrings) != len(secondStrings) {
		return false
	}
	sort.Strings(firstStrings)
	sort.Strings(secondStrings)
	for i, first := range firstStrings {
		if first != secondStrings[i] {
			return false
		}
	}
	return true
}

func executeStatus(t *testing.T, fake *jiritest.FakeJiriRoot, args ...string) string {
	stderr := ""
	runCmd := func() {
		if err := runStatus(fake.X, args); err != nil {
			stderr = err.Error()
		}
	}
	stdout, _, err := runfunc(runCmd)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(strings.Join([]string{stdout, stderr}, " "))
}

func writeFile(t *testing.T, jirix *jiri.X, projectDir, fileName, message string) {
	path, perm := filepath.Join(projectDir, fileName), os.FileMode(0644)
	if err := ioutil.WriteFile(path, []byte(message), perm); err != nil {
		t.Fatalf("WriteFile(%v, %v) failed: %v", path, perm, err)
	}
	if err := gitutil.New(jirix.NewSeq(), gitutil.RootDirOpt(projectDir)).CommitFile(path, message); err != nil {
		t.Fatal(err)
	}
}

func setDummyUser(t *testing.T, jirix *jiri.X, projectDir string) {
	git := gitutil.New(jirix.NewSeq(), gitutil.RootDirOpt(projectDir))
	if err := git.Config("user.email", "john.doe@example.com"); err != nil {
		t.Fatalf("%v", err)
	}
	if err := git.Config("user.name", "John Doe"); err != nil {
		t.Fatalf("%v", err)
	}
}
