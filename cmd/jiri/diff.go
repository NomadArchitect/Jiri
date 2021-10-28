// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/gerrit"
	"go.fuchsia.dev/jiri/log"
	"go.fuchsia.dev/jiri/project"
)

var diffFlags struct {
	cls          bool
	indentOutput bool

	// Need this to avoid infinite loop
	maxCls uint
}

var cmdDiff = &cmdline.Command{
	Runner:   jiri.RunnerFunc(runDiff),
	Name:     "diff",
	Short:    "Prints diff between two snapshots",
	ArgsName: "<snapshot-1> <snapshot-2>",
	ArgsLong: "<snapshot-1/2> are files or urls containing snapshot",
	Long: `
Prints diff between two snapshots in json format. Max CLs returned for a
project is controlled by flag max-xls and is default by 5. The format of
returned json:
{
	new_projects: [
		{
			name: name,
			path: path,
			relative_path: relative-path,
			remote: remote,
			revision: rev
		},{...}...
	],
	deleted_projects:[
		{
			name: name,
			path: path,
			relative_path: relative-path,
			remote: remote,
			revision: rev
		},{...}...
	],
	updated_projects:[
		{
			name: name,
			path: path,
			relative_path: relative-path,
			remote: remote,
			revision: rev
			old_revision: old-rev, // if updated
			old_path: old-path //if moved
			old_relative_path: old-relative-path //if moved
			cls:[
				{
					number: num,
					url: url,
					commit: commit,
					subject:sub
				},{...},...
			]
			has_more_cls: true,
			error: error in retrieving CL
		},{...}...
	]
}
`,
}

func init() {
	flags := &cmdDiff.Flags
	flags.BoolVar(&diffFlags.cls, "cls", true, "Return CLs for changed projects")
	flags.BoolVar(&diffFlags.indentOutput, "indent", true, "Indent json output")
	flags.UintVar(&diffFlags.maxCls, "max-cls", 5, "Max number of CLs returned per changed project")
}

type DiffCl struct {
	Commit  string `json:"commit"`
	Number  int    `json:"number"`
	Subject string `json:"subject"`
	URL     string `json:"url"`
}

type DiffProject struct {
	Name            string   `json:"name"`
	Remote          string   `json:"remote"`
	Path            string   `json:"path"`
	RelativePath    string   `json:"relative_path"`
	OldPath         string   `json:"old_path,omitempty"`
	OldRelativePath string   `json:"old_relative_path,omitempty"`
	Revision        string   `json:"revision"`
	OldRevision     string   `json:"old_revision,omitempty"`
	Cls             []DiffCl `json:"cls,omitempty"`
	Error           string   `json:"error,omitempty"`
	HasMoreCls      bool     `json:"has_more_cls,omitempty"`
}

type DiffProjectsByName []DiffProject

func (p DiffProjectsByName) Len() int {
	return len(p)
}
func (p DiffProjectsByName) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}
func (p DiffProjectsByName) Less(i, j int) bool {
	return p[i].Name < p[j].Name
}

type DiffPackage struct {
	Name            string   `json:"name"`
	Path            string   `json:"path"`
	RelativePath    string   `json:"relative_path"`
	OldPath         string   `json:"old_path,omitempty"`
	OldRelativePath string   `json:"old_relative_path,omitempty"`
	Version         string   `json:"version"`
	OldVersion      string   `json:"old_version,omitempty"`
}

type DiffPackagesByName []DiffPackage

func (p DiffPackagesByName) Len() int {
	return len(p)
}
func (p DiffPackagesByName) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}
func (p DiffPackagesByName) Less(i, j int) bool {
	return p[i].Name < p[j].Name
}

type Diff struct {
	NewProjects     []DiffProject `json:"new_projects"`
	DeletedProjects []DiffProject `json:"deleted_projects"`
	UpdatedProjects []DiffProject `json:"updated_projects"`
	NewPackages     []DiffPackage `json:"new_packages"`
	DeletedPackages []DiffPackage `json:"deleted_packages"`
	UpdatedPackages []DiffPackage `json:"updated_packages"`
}

func (d *Diff) Sort() *Diff {
	sort.Sort(DiffProjectsByName(d.NewProjects))
	sort.Sort(DiffProjectsByName(d.DeletedProjects))
	sort.Sort(DiffProjectsByName(d.UpdatedProjects))
	sort.Sort(DiffPackagesByName(d.NewPackages))
	sort.Sort(DiffPackagesByName(d.DeletedPackages))
	sort.Sort(DiffPackagesByName(d.UpdatedPackages))
	return d
}

func runDiff(jirix *jiri.X, args []string) error {
	if len(args) != 2 {
		return jirix.UsageErrorf("Please provide two snapshots to diff")
	}
	d, err := getDiff(jirix, args[0], args[1])
	if err != nil {
		return err
	}
	e := json.NewEncoder(os.Stdout)
	if diffFlags.indentOutput {
		e.SetIndent("", " ")
	}
	return e.Encode(d)
}

func getDiff(jirix *jiri.X, snapshot1, snapshot2 string) (*Diff, error) {
	diff := &Diff{
		NewProjects:     make([]DiffProject, 0),
		DeletedProjects: make([]DiffProject, 0),
		UpdatedProjects: make([]DiffProject, 0),
	}
	oldLogger := jirix.Logger
	defer func() {
		jirix.Logger = oldLogger
	}()
	jirix.Logger = log.NewLogger(log.NoLogLevel, jirix.Color, false, 0, oldLogger.TimeLogThreshold(), nil, nil)
	projects1, _, packages1, err := project.LoadSnapshotFile(jirix, snapshot1)
	if err != nil {
		return nil, err
	}
	projects2, _, packages2, err := project.LoadSnapshotFile(jirix, snapshot2)
	if err != nil {
		return nil, err
	}
	project.MatchLocalWithRemote(projects1, projects2)
	jirix.Logger = oldLogger

	// Get deleted projects
	for key, p1 := range projects1 {
		if _, ok := projects2[key]; !ok {
			rp, err := filepath.Rel(jirix.Root, p1.Path)
			if err != nil {
				// should not happen
				panic(err)
			}
			diff.DeletedProjects = append(diff.DeletedProjects, DiffProject{
				Name:         p1.Name,
				Remote:       p1.Remote,
				Path:         p1.Path,
				RelativePath: rp,
				Revision:     p1.Revision,
			})
		}
	}

	// Get new projects and also extract updated projects
	updatedProjectKeys := make(chan project.ProjectKey, len(projects2))
	for key, p2 := range projects2 {
		if p1, ok := projects1[key]; !ok {
			rp, err := filepath.Rel(jirix.Root, p2.Path)
			if err != nil {
				// should not happen
				panic(err)
			}

			diff.NewProjects = append(diff.NewProjects, DiffProject{
				Name:         p2.Name,
				Remote:       p2.Remote,
				Path:         p2.Path,
				RelativePath: rp,
				Revision:     p2.Revision,
			})
		} else {
			if p1.Path != p2.Path || p1.Revision != p2.Revision {
				updatedProjectKeys <- key
			}
		}
	}

	close(updatedProjectKeys)

	processUpdatedProject := func(key project.ProjectKey) DiffProject {
		p1 := projects1[key]
		p2 := projects2[key]
		rp, err := filepath.Rel(jirix.Root, p2.Path)
		if err != nil {
			// should not happen
			panic(err)
		}
		diffP := DiffProject{
			Name:         p2.Name,
			Remote:       p2.Remote,
			Path:         p2.Path,
			RelativePath: rp,
			Revision:     p2.Revision,
		}
		if p1.Path != p2.Path {
			rp, err := filepath.Rel(jirix.Root, p1.Path)
			if err != nil {
				// should not happen
				panic(err)
			}
			diffP.OldPath = p1.Path
			diffP.OldRelativePath = rp
		}
		if p1.Revision != p2.Revision {
			diffP.OldRevision = p1.Revision
			if !diffFlags.cls {
				// do nothing, prevents nested if/else
			} else if p2.GerritHost == "" {
				diffP.Error = "no gerrit host"
			} else if hostUrl, err := url.Parse(p1.GerritHost); err != nil {
				diffP.Error = fmt.Sprintf("invalid gerrit host %q: %s", p2.GerritHost, err)
			} else {
				g := gerrit.New(jirix, hostUrl)
				revision := p2.Revision
				for i := uint(0); i < diffFlags.maxCls && revision != p1.Revision; i++ {
					cls, err := g.ListChangesByCommit(revision)
					if err != nil {
						diffP.Error = fmt.Sprintf("not able to get CL for revision %s: %s", revision, err)
						break
					}
					var cl *gerrit.Change
					for _, c := range cls {
						if c.Current_revision == revision {
							cl = &c
							break
						}
					}
					if cl == nil {
						diffP.Error = fmt.Sprintf("not able to get CL for revision %s", revision)
						break
					}
					diffCl := DiffCl{
						Commit:  revision,
						Number:  cl.Number,
						Subject: cl.Subject,
						URL:     fmt.Sprintf("%s/c/%d", p2.GerritHost, cl.Number),
					}
					diffP.Cls = append(diffP.Cls, diffCl)
					parents := cl.Revisions[revision].Parents
					if len(parents) != 1 {
						if len(parents) == 0 {
							diffP.Error = fmt.Sprintf("not able to get parent for revision %s", revision)
							break
						} else if len(parents) > 1 {
							diffP.Error = fmt.Sprintf("more than one parent for revision %s", revision)
							break
						}
					}
					revision = parents[0].Commit
				}
				if revision != p1.Revision && diffP.Error == "" {
					diffP.HasMoreCls = true
				}
			}
		}
		return diffP
	}

	diffs := make(chan DiffProject, len(updatedProjectKeys))
	var wg sync.WaitGroup
	for i := uint(0); i < jirix.Jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range updatedProjectKeys {
				diffs <- processUpdatedProject(key)
			}
		}()
	}
	wg.Wait()
	close(diffs)
	for diffP := range diffs {
		diff.UpdatedProjects = append(diff.UpdatedProjects, diffP)
	}

	// Get deleted packages
	for key, p1 := range packages1 {
		if _, ok := packages2[key]; !ok {
			rp, err := filepath.Rel(jirix.Root, p1.Path)
			if err != nil {
				// should not happen
				panic(err)
			}
			diff.DeletedPackages = append(diff.DeletedPackages, DiffPackage{
				Name:         p1.Name,
				Path:         p1.Path,
				RelativePath: rp,
				Version:     p1.Version,
			})
		}
	}

	// Get new packages lso extract updated packages
	updatedPackageKeys := make(chan project.PackageKey, len(packages2))
	for key, p2 := range packages2 {
		if p1, ok := packages1[key]; !ok {
			rp, err := filepath.Rel(jirix.Root, p2.Path)
			if err != nil {
				// should not happen
				panic(err)
			}

			diff.NewPackages = append(diff.NewPackages, DiffPackage{
				Name:         p2.Name,
				Path:         p2.Path,
				RelativePath: rp,
				Version:      p2.Version,
			})
		} else {
			if p1.Path != p2.Path || p1.Version != p2.Version {
				updatedPackageKeys <- key
			}
		}
	}

	close(updatedPackageKeys)

	processUpdatedPackage := func(key project.PackageKey) DiffPackage {
		p1 := packages1[key]
		p2 := packages2[key]
		rp, err := filepath.Rel(jirix.Root, p2.Path)
		if err != nil {
			// should not happen
			panic(err)
		}
		diffP := DiffPackage{
			Name:         p2.Name,
			Path:         p2.Path,
			RelativePath: rp,
			Version:     p2.Version,
		}
		if p1.Path != p2.Path {
			rp, err := filepath.Rel(jirix.Root, p1.Path)
			if err != nil {
				// should not happen
				panic(err)
			}
			diffP.OldPath = p1.Path
			diffP.OldRelativePath = rp
		}
		if p1.Version != p2.Version {
			diffP.OldVersion = p1.Version
		}
		return diffP
	}

	diffsPkg := make(chan DiffPackage, len(updatedPackageKeys))
	var wgPack sync.WaitGroup
	for i := uint(0); i < jirix.Jobs; i++ {
		wgPack.Add(1)
		go func() {
			defer wgPack.Done()
			for key := range updatedPackageKeys {
				diffsPkg <- processUpdatedPackage(key)
			}
		}()
	}
	wgPack.Wait()
	close(diffsPkg)
	for diffP := range diffsPkg {
		diff.UpdatedPackages = append(diff.UpdatedPackages, diffP)
	}

	return diff.Sort(), nil
}
