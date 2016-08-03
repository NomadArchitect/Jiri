// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package profilesreader_test

import (
	"flag"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"fuchsia.googlesource.com/jiri/jiritest"
	"fuchsia.googlesource.com/jiri/profiles"
	"fuchsia.googlesource.com/jiri/profiles/profilesreader"
	"fuchsia.googlesource.com/jiri/envvar"
)

func TestReader(t *testing.T) {
	fake, cleanup := jiritest.NewFakeJiriRoot(t)
	defer cleanup()
	rd, err := profilesreader.NewReader(fake.X, profilesreader.UseProfiles, filepath.Join("testdata", "m2.xml"))
	if err != nil {
		t.Fatal(err)
	}
	rd.Vars = envvar.VarsFromOS()
	rd.Delete("CGO_CFLAGS")
	native, err := profiles.NewTarget("amd64-darwin", "")
	if err != nil {
		t.Fatal(err)
	}
	rd.MergeEnvFromProfiles(profilesreader.JiriMergePolicies(), native, "test::go", "test::syncbase")
	if got, want := rd.Get("CGO_CFLAGS"), "-IX -IY -IA -IB"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := rd.DebugString(), "Root: "+fake.X.Root+`
Path: testdata/m2.xml
test:go: amd64-darwin@1.5 dir: --env= envvars:[CGO_ENABLED=1 GOARCH=amd64 GOOS=darwin GOROOT=/goroot CGO_CFLAGS=-IX -IY]
test:syncbase: amd64-darwin@1 dir: --env= envvars:[CGO_CFLAGS=-IA -IB CGO_LDFLAGS=-LA -LB]
`; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEnvFromTarget(t *testing.T) {
	fake, cleanup := jiritest.NewFakeJiriRoot(t)
	defer cleanup()
	pdb := profiles.NewDB()
	pdb.InstallProfile("test", "a", "root")
	pdb.InstallProfile("test", "b", "root")
	t1, t2 := &profiles.Target{}, &profiles.Target{}

	t1.Set("cpu1-os1@1")
	t1.Env.Set("A=B C=D,B=C Z=Z")
	t2.Set("cpu1-os1@1")
	t2.Env.Set("A=Z,B=Z,Z=Z1")
	pdb.AddProfileTarget("test", "a", *t1)
	pdb.AddProfileTarget("test", "b", *t2)
	pdb.Write(fake.X, "test", "profile-manifest")
	filename := filepath.Join(fake.X.Root, "profile-manifest")
	if err := pdb.Write(fake.X, "test", filename); err != nil {
		t.Fatal(err)
	}
	rd, err := profilesreader.NewReader(fake.X, profilesreader.UseProfiles, filename)
	if err != nil {
		t.Fatal(err)
	}
	rd.Vars = envvar.VarsFromSlice([]string{})
	t1Target, err := profiles.NewTarget("cpu1-os1@1", "")
	if err != nil {
		t.Fatal(err)
	}
	rd.MergeEnvFromProfiles(map[string]profilesreader.MergePolicy{
		"A": profilesreader.AppendFlag,
		"B": profilesreader.UseLast,
		"Z": profilesreader.IgnoreBaseUseLast},
		t1Target, "test::a", "test::b")
	vars := rd.ToMap()
	if got, want := len(vars), 3; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := rd.Get("A"), "B C=D Z"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := rd.Get("B"), "Z"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeEnv(t *testing.T) {
	base := []string{"FS1=A", "IF=A", "A=B", "B=A", "C=D", "P=A", "V=A", "P1=A", "V1=A", "IA=A", "IB=A", "IC=A", "ID=A", "IE=A", "IG1=A"}
	b := []string{"FS1=B", "FS2=B", "IF=B", "A=B1", "B=B", "C=D1", "P=B", "V=B", "P1=B", "V1=B", "W=X", "Y=Z", "GP=X", "IA=B", "IB=B", "IC=B", "ID=B", "IE=B", "IG2=A"}
	c := []string{"FS1=C", "FS2=C", "FS3=C", "A=BL", "B=C", "C=DL", "P=C", "V=C", "P1=C", "V1=C", "Y=ZL", "GP=XL", "IA=C", "IB=C", "IC=C", "ID=C", "IE=C", "IG3=B"}
	env := envvar.VarsFromSlice(base)

	policies := map[string]profilesreader.MergePolicy{
		"GP":  profilesreader.UseLast,
		"P":   profilesreader.PrependPath,
		"V":   profilesreader.PrependFlag,
		"P1":  profilesreader.AppendPath,
		"V1":  profilesreader.AppendFlag,
		"A":   profilesreader.IgnoreBaseUseLast,
		"B":   profilesreader.UseBaseIgnoreProfiles,
		"IA":  profilesreader.IgnoreBaseAppendPath,
		"IB":  profilesreader.IgnoreBaseAppendFlag,
		"IC":  profilesreader.IgnoreBasePrependPath,
		"ID":  profilesreader.IgnoreBasePrependFlag,
		"IE":  profilesreader.IgnoreBaseUseLast,
		"IF":  profilesreader.IgnoreBaseUseFirst,
		"IG1": profilesreader.IgnoreVariable,
		"IG2": profilesreader.IgnoreVariable,
		"IG3": profilesreader.IgnoreVariable,
		"C":   profilesreader.UseLast,
		"Y":   profilesreader.UseLast,
	}
	profilesreader.MergeEnv(policies, env, b, c)

	expected := []string{"B=A", "A=BL", "C=DL", "GP=XL", "P1=A:B:C", "P=C:B:A",
		"V1=A B C", "V=C B A", "W=X", "Y=ZL",
		"IA=B:C", "IB=B C", "IC=C:B", "ID=C B", "IE=C",
		"FS1=A", "FS2=B", "FS3=C", "IF=B",
	}
	sort.Strings(expected)
	if got, want := env.ToSlice(), expected; len(got) != len(want) {
		sort.Strings(got)
		t.Errorf("got: %v", got)
		t.Errorf("want: %v", want)
		t.Errorf("got %v, want %v", len(got), len(want))
	}
	for _, g := range env.ToSlice() {
		found := false
		for _, w := range expected {
			if g == w {
				found = true
			}
		}
		if !found {
			t.Errorf("failed to find %v in %v", g, expected)
		}
	}
}

func TestMergePolicyFlags(t *testing.T) {
	mp := profilesreader.MergePolicies{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Var(mp, "p", mp.Usage())
	all := []string{"-p=:a", "-p=+b", "-p=^c", "-p=^:d", "-p=^e:", "-p=^+f", "-p=^g+", "-p=last*", "-p=xx:", "-p=yy+", "-p=zz^"}
	if err := fs.Parse(all); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		k string
		p profilesreader.MergePolicy
	}{
		{"a", profilesreader.AppendPath},
		{"b", profilesreader.AppendFlag},
		{"c", profilesreader.IgnoreBaseUseFirst},
		{"d", profilesreader.IgnoreBaseAppendPath},
		{"e", profilesreader.IgnoreBasePrependPath},
		{"f", profilesreader.IgnoreBaseAppendFlag},
		{"g", profilesreader.IgnoreBasePrependFlag},
		{"last", profilesreader.UseLast},
		{"xx", profilesreader.PrependPath},
		{"yy", profilesreader.PrependFlag},
		{"zz", profilesreader.UseBaseIgnoreProfiles},
	} {
		if got, want := mp[c.k], c.p; got != want {
			t.Errorf("(%s) got %v, want %v", c.k, got, want)
		}
	}

	mp = profilesreader.MergePolicies{}
	fs1 := flag.NewFlagSet("test1", flag.ContinueOnError)
	fs1.Var(mp, "p", mp.Usage())
	if err := fs1.Parse([]string{"-p=yy+,zz^"}); err != nil {
		t.Fatal(err)
	}
	if got, want := len(mp), 2; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	for i, cl := range append(all, "-p=+b,^c,zz^") {
		mp := profilesreader.MergePolicies{}
		fs := flag.NewFlagSet(fmt.Sprintf("t%d", i), flag.ContinueOnError)
		fs.Var(mp, "p", mp.Usage())
		err := fs.Parse([]string{cl})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := "-p="+mp.String(), cl; got != want {
			t.Errorf("%d: got %v, want %v", i, got, want)
		}
	}
}
