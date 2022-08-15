// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package project

import (
	"regexp"
	"strings"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/gitutil"
)

type Submodule struct {
	Name string
	Path string
	// Submodule SHA-1 prefix, could be "+", "-" or "U".
	// "-" means submodule not initialized.
	Prefix       string
	Remote       string
	Revision     string
	Superproject string
}

type Submodules map[string]Submodule

var submoduleConfigRegex = regexp.MustCompile(`[-+U]?([a-fA-F0-9]{40})\s(.*?)\s`)
var submodulePrefixRegex = regexp.MustCompile(`([-+U])?`)

// containSubmodules checks if any of the projects contain submodules.
func containSubmodules(jirix *jiri.X, projects Projects) bool {
	for _, p := range projects {
		if p.GitSubmodules {
			if isSuperproject(jirix, p) {
				return true
			}
		}
	}
	return false
}

// getAllSubmodules return all submodules states.
func getAllSubmodules(jirix *jiri.X, projects Projects) []Submodules {
	var allSubmodules []Submodules
	for _, p := range projects {
		if p.GitSubmodules {
			if submodules := getSubmodulesStatus(jirix, p); submodules != nil {
				allSubmodules = append(allSubmodules, submodules)
			}
		}
	}
	return allSubmodules
}

// getSubmoduleStatus returns submodule states in superproject.
func getSubmodulesStatus(jirix *jiri.X, superproject Project) Submodules {
	scm := gitutil.New(jirix, gitutil.RootDirOpt(superproject.Path))
	submoduleStatus, _ := scm.SubmoduleStatus()
	submodules := make(Submodules)
	for _, submodule := range submoduleStatus {

		submConfig := submoduleConfigRegex.FindAllString(submodule, 1)[0]
		subm := Submodule{
			Prefix:       submodulePrefixRegex.FindAllString(submodule, 1)[0],
			Revision:     strings.Split(submConfig, " ")[0],
			Name:         strings.Split(submConfig, " ")[1],
			Superproject: superproject.Name,
		}
		subm.Remote, _ = scm.SubmoduleURL(subm.Name)
		submodules[subm.Name] = subm
		if subm.Prefix == "+" {
			jirix.Logger.Warningf("Submodule %s current checkout does not match the SHA-1 to the index of the containing repository.", subm.Name)
		}
		if subm.Prefix == "U" {
			jirix.Logger.Warningf("Submodule %s has merge conflicts.", subm.Name)
		}
	}
	return submodules
}

// getSuperprojectStates returns the superprojects that have submodules enabled.
func getSuperprojectStates(projects Projects) map[string]bool {
	superprojectStates := make(map[string]bool)
	for _, p := range projects {
		if p.GitSubmodules {
			superprojectStates[p.Name] = true
		}
	}
	return superprojectStates
}

// isSuperproject checks if submodules exist under a project
func isSuperproject(jirix *jiri.X, project Project) bool {
	submodules := getSubmodulesStatus(jirix, project)
	for _, subm := range submodules {
		if subm.Prefix != "-" {
			return true
		}
	}
	return false
}

// removeSubmodulesFromProjects removes verified submodules from jiri projects.
func removeSubmodulesFromProjects(projects Projects) Projects {
	var submoduleProjectKeys []ProjectKey
	superprojectStates := getSuperprojectStates(projects)
	for k, p := range projects {
		if superprojectStates[p.GitSubmoduleOf] {
			submoduleProjectKeys = append(submoduleProjectKeys, k)
		}
	}
	for _, k := range submoduleProjectKeys {
		delete(projects, k)
	}
	return projects
}
