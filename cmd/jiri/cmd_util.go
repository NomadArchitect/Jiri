// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/project"
)

// currentProject returns the Project containing the current working directory.
// The current working directory must be inside root.
func currentProject(jirix *jiri.X) (project.Project, error) {
	dir, err := os.Getwd()
	if err != nil {
		return project.Project{}, fmt.Errorf("os.Getwd() failed: %s", err)
	}

	// Walk up the path until we find a project at that path, or hit the jirix.Root.
	// Note that we can't just compare path prefixes because of soft links.
	for dir != jirix.Root && dir != string(filepath.Separator) {
		p, err := project.ProjectAtPath(jirix, dir)
		if err != nil {
			dir = filepath.Dir(dir)
			continue
		}
		return p, nil
	}
	return project.Project{}, fmt.Errorf("directory %q is not contained in a project", dir)
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
