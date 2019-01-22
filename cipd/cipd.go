// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cipd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/osutil"
	"fuchsia.googlesource.com/jiri/version"
)

const (
	cipdBackend       = "https://chrome-infra-packages.appspot.com"
	cipdVersion       = "git_revision:00e2d8b49a4e7505d1c71f19d15c9e7c5b9245a5"
	cipdVersionDigest = `
# This file was generated by
#
#  cipd selfupdate-roll -version-file .cipd_version \
#      -version git_revision:00e2d8b49a4e7505d1c71f19d15c9e7c5b9245a5
#
# Do not modify manually. All changes will be overwritten.
# Use 'cipd selfupdate-roll ...' to modify.
linux-386       sha256  15c8c9c96ae1dfc1fff8ce5469eecb60dded782f6b8b7ba37d9edbeca443017e
linux-amd64     sha256  df37ffc2588e345a31ca790d773b6136fedbd2efbf9a34cb735dd34b6891c16c
linux-arm64     sha256  650f2a045f8587062a16299a650aa24ba5c5c0652585a2d9bd56594369d5f99e
linux-armv6l    sha256  61b657c860ddc39d3286ced073c843852b1dafc0222af0bdc22ad988b289d733
linux-mips64    sha256  ab58e3886e8e0805c6c19e58b2f5a96e9fa3921c9dd1fc3ee956bf07b35993df
linux-mips64le  sha256  acac10fdb9fd459ef318e6b10b971471e2b6814e4a7fe14f119a15bd89b4b163
linux-mipsle    sha256  a2d31d02c838cbd849b068fa371ec278a836d2f184a90fb9d1a63271cf152d82
linux-ppc64     sha256  a6f5a84de095bbe986dda8a16d17525280fc448d63347fd240c71d1cead78036
linux-ppc64le   sha256  77afc938916f84eec342acaabb8e32a845eac72e23b9db018cdf09fc31df585b
linux-s390x     sha256  86dd1092d1dc228d59d3862bb2d48ebe2e4f16a3c054fb04391f07d9d2b15d33
mac-amd64       sha256  4d015791ed6f03f305cf6a5a673a447e5c47ff5fdb701f43f99fba9ca73e61f8
windows-386     sha256  b8102c9a1b93915c128e7577c89fd77991ab83d52c356913e56ea505ab338735
windows-amd64   sha256  a117e3984c111c68698faf91815c4b7d374404fa82dff318aadb9f2f0582ca8d
`
	cipdNotLoggedInStr = "Not logged in"
)

var (
	cipdPlatform   string
	cipdOS         string
	cipdArch       string
	cipdBinary     string
	selfUpdateOnce sync.Once
	templateRE     = regexp.MustCompile(`\${[^}]*}`)

	// ErrSkipTemplate may be returned from Expander.Expand to indicate that
	// a given expansion doesn't apply to the current template parameters. For
	// example, expanding `"foo/${os=linux,mac}"` with a template parameter of "os"
	// == "win", would return ErrSkipTemplate.
	ErrSkipTemplate = errors.New("package template does not apply to the current system")
)

func init() {
	cipdOS = runtime.GOOS
	cipdArch = runtime.GOARCH
	if cipdOS == "darwin" {
		cipdOS = "mac"
	}
	if cipdArch == "arm" {
		cipdArch = "armv6l"
	}
	cipdPlatform = cipdOS + "-" + cipdArch

	jiriPath, err := osutil.Executable()
	if err != nil {
		// Could not locate jiri, leave cipdBinary empty
		// It will be checked by bootstrap and reported
		return
	}
	// Assume cipd binary is located in the same directory of jiri
	jiriBinaryRoot := path.Dir(jiriPath)
	cipdBinary = path.Join(jiriBinaryRoot, "cipd")
}

func fetchBinary(binaryPath, platform, version, digest string) error {
	cipdURL := fmt.Sprintf("%s/client?platform=%s&version=%s", cipdBackend, platform, version)
	data, err := fetchFile(cipdURL)
	if err != nil {
		return err
	}
	if verified, err := verifyDigest(data, digest); err != nil || !verified {
		if err != nil {
			return err
		}
		return errors.New("cipd failed integrity test")
	}
	// cipd binary verified. Save to disk
	return writeFile(binaryPath, data)
}

// Bootstrap returns the path of a valid cipd binary. It will fetch cipd from
// remote if a valid cipd binary is not found. It will update cipd if there
// is a new version.
func Bootstrap() (string, error) {
	bootstrap := func() error {
		// Fetch cipd digest
		cipdDigest, _, err := fetchDigest(cipdPlatform)
		if err != nil {
			return err
		}
		if cipdBinary == "" {
			return errors.New("cipd binary path was not set")
		}
		if err != nil {
			return err
		}
		return fetchBinary(cipdBinary, cipdPlatform, cipdVersion, cipdDigest)
	}

	getCipd := func() (string, error) {
		if cipdBinary == "" {
			return "", errors.New("cipd binary path was not set")
		}
		fileInfo, err := os.Stat(cipdBinary)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("cipd binary was not found at %q", cipdBinary)
			}
			return "", err
		}
		// Check if cipd binary has execution permission
		if fileInfo.Mode()&0111 == 0 {
			return "", fmt.Errorf("cipd binary at %q is not executable", cipdBinary)
		}
		return cipdBinary, nil
	}

	cipdPath, err := getCipd()
	if err != nil {
		// Could not find cipd binary or cipd is invalid
		// Bootstrap it from scratch
		if err := bootstrap(); err != nil {
			return "", err
		}
		return cipdBinary, nil
	}
	// cipd is found, do self update
	var e error
	selfUpdateOnce.Do(func() {
		e = selfUpdate(cipdPath, cipdVersion)
	})
	if e != nil {
		// Self update is unsuccessful, redo bootstrap
		if err := bootstrap(); err != nil {
			return "", err
		}
	}
	return cipdPath, nil
}

func fetchDigest(platform string) (digest, method string, err error) {
	var digestBuf bytes.Buffer
	digestBuf.Write([]byte(cipdVersionDigest))
	digestScanner := bufio.NewScanner(&digestBuf)
	for digestScanner.Scan() {
		curLine := digestScanner.Text()
		if len(curLine) == 0 || curLine[0] == '#' {
			// Skip comment or empty line
			continue
		}
		fields := strings.Fields(curLine)
		if len(fields) != 3 {
			return "", "", errors.New("unsupported cipd digest file format")
		}
		if fields[0] == platform {
			digest = fields[2]
			method = fields[1]
			err = nil
			return
		}
	}
	return "", "", errors.New("no matching platform found in cipd digest file")
}

func selfUpdate(cipdPath, cipdVersion string) error {
	args := []string{"selfupdate", "-version", cipdVersion, "-service-url", cipdBackend}
	command := exec.Command(cipdPath, args...)
	return command.Run()
}

func writeFile(filePath string, data []byte) error {
	tempFile, err := ioutil.TempFile(path.Dir(filePath), "cipd.*")
	if err != nil {
		return err
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())
	if _, err := tempFile.Write(data); err != nil {
		// Write errors
		return errors.New("I/O error while downloading cipd binary")
	}
	// Set mode to rwxr-xr-x
	if err := tempFile.Chmod(0755); err != nil {
		// Chmod errors
		return errors.New("I/O error while adding executable permission to cipd binary")
	}
	tempFile.Close()
	if err := os.Rename(tempFile.Name(), filePath); err != nil {
		return err
	}
	return nil
}

func verifyDigest(data []byte, cipdDigest string) (bool, error) {
	hash := sha256.Sum256(data)
	hashString := fmt.Sprintf("%x", hash)
	if hashString == strings.ToLower(cipdDigest) {
		return true, nil
	}
	return false, nil
}

func getUserAgent() string {
	ua := "jiri/" + version.GitCommit
	if version.GitCommit == "" {
		ua += "debug"
	}
	return ua
}

func fetchFile(url string) ([]byte, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", getUserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

type packageACL struct {
	path   string
	access bool
}

func checkPackageACL(jirix *jiri.X, path, version string, c chan<- packageACL) {
	// cipd should be already bootstrapped before this go routine.
	// Silently return a false just in case if cipd is not found.
	if cipdBinary == "" {
		c <- packageACL{path: path, access: false}
		return
	}

	args := []string{"resolve", path, "-version", version}
	if jirix != nil {
		jirix.Logger.Debugf("Invoke cipd with %v", args)
	}
	command := exec.Command(cipdBinary, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf
	// Return false if cipd cannot be executed or cipd returned a non-zero
	// return code, which usually means the package cannot be found due to
	// access control.
	if err := command.Run(); err != nil {
		if jirix != nil {
			jirix.Logger.Debugf("Error happend while executing cipd, err: %q, stderr: %q", err, stderrBuf.String())
		}
		c <- packageACL{path: path, access: false}
		return
	}
	// cipd returned zero. Package can be accessed.
	c <- packageACL{path: path, access: true}
	return
}

// CheckPackageACL checks cipd's access to packages in map "pkgs". The package
// names in "pkgs" should have trailing '/' removed before calling this
// function.
func CheckPackageACL(jirix *jiri.X, pkgs map[string]bool, versions map[string]string) error {
	// Not declared as CheckPackageACL(jirix *jiri.X, pkgs map[*package.Package]bool)
	// due to import cycles. Package jiri/package imports jiri/cipd so here we cannot
	// import jiri/package.
	if _, err := Bootstrap(); err != nil {
		return err
	}

	c := make(chan packageACL)
	for key := range pkgs {
		go checkPackageACL(jirix, key, versions[key], c)
	}

	for i := 0; i < len(pkgs); i++ {
		acl := <-c
		pkgs[acl.path] = acl.access
	}
	return nil
}

// CheckLoggedIn checks cipd's user login information. It will return true
// if login information is found or return false if login information is not
// found.
func CheckLoggedIn(jirix *jiri.X) (bool, error) {
	cipdPath, err := Bootstrap()
	if err != nil {
		return false, err
	}
	args := []string{"auth-info"}
	command := exec.Command(cipdPath, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf
	if err := command.Run(); err != nil {
		stdErrMsg := strings.TrimSpace(stderrBuf.String())
		if jirix != nil {
			jirix.Logger.Debugf("Error happend while executing cipd, err: %q, stderr: %q", err, stdErrMsg)
		}
		if _, ok := err.(*exec.ExitError); ok && stdErrMsg == cipdNotLoggedInStr {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Ensure runs cipd binary's ensure funcationality over file. Fetched packages will be
// saved to projectRoot directory. Parameter timeout is in minutes.
func Ensure(jirix *jiri.X, file, projectRoot string, timeout uint) error {
	cipdPath, err := Bootstrap()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Minute)
	defer cancel()
	args := []string{"ensure", "-ensure-file", file, "-root", projectRoot, "-log-level", "warning"}
	// Workaround so tests do not have to create a new fake jirix, which would
	// result in an import cycle
	if jirix != nil {
		jirix.Logger.Debugf("Invoke cipd with %v", args)
	}
	// Construct arguments and invoke cipd for ensure file
	command := exec.CommandContext(ctx, cipdPath, args...)
	// Add User-Agent info for cipd
	command.Env = append(os.Environ(), "CIPD_HTTP_USER_AGENT_PREFIX="+getUserAgent())
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	return command.Run()
}

// TODO: Using PackageLock in project package directly will cause an import
// cycle. Remove this type once we solve the this issue.

// PackageInstance describes package instance id information generated by cipd
// ensure-file-resolve. It is a copy of PackageLock type in project package.
type PackageInstance struct {
	PackageName string
	VersionTag  string
	InstanceID  string
}

// Resolve runs cipd binary's ensure-file-resolve functionality over file.
// It returns a slice containing resolved packages and cipd instance ids.
func Resolve(jirix *jiri.X, file string) ([]PackageInstance, error) {
	cipdPath, err := Bootstrap()
	if err != nil {
		return nil, err
	}
	args := []string{"ensure-file-resolve", "-ensure-file", file, "-log-level", "warning"}
	// Workaround so tests do not have to create a new fake jirix, which would
	// result in an import cycle
	if jirix != nil {
		jirix.Logger.Debugf("Invoke cipd with %v", args)
	}
	command := exec.Command(cipdPath, args...)
	command.Env = append(os.Environ(), "CIPD_HTTP_USER_AGENT_PREFIX="+getUserAgent())
	var stdoutBuf, stderrBuf bytes.Buffer
	command.Stdin = os.Stdin
	// Redirect outputs since cipd will print verbose information even
	// if log-level is set to warning
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf
	if err := command.Run(); err != nil {
		if jirix != nil {
			jirix.Logger.Errorf("cipd returned error: %v", stderrBuf.String())
		}
		return nil, err
	}

	// cipd generates the version file in the same directory of the ensure file
	// if no error is returned
	versionFile := file[:len(file)-len(".ensure")] + ".version"
	defer os.Remove(versionFile)
	return parseVersions(versionFile)
}

func parseVersions(file string) ([]PackageInstance, error) {
	versionReader, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer versionReader.Close()
	versionScanner := bufio.NewScanner(versionReader)
	// An example cipd version looks like:
	// ==========================================================
	// # Do not modify manually. All changes will be overwritten.
	// fuchsia/clang/linux-amd64
	// 	git_revision:280fa3c2d2ddb0b5dcb31113c0b1c2259982b7e7
	// 	eRoGS8qgx370QAIRgLDmbhpdPey8ti47B2Z3LMzwcXQC
	//
	// fuchsia/clang/mac-amd64
	// 	git_revision:280fa3c2d2ddb0b5dcb31113c0b1c2259982b7e7
	// 	BQhlnpoWG081CyLzA0zB1vCr8YPdb2DO2jnYe3Lsw4oC
	// ===========================================================
	// Parse version file using DFA

	const (
		stWaitingPkg = "a package name"
		stWaitingVer = "a package version"
		stWaitingIID = "an instance ID"
		stWaitingNL  = "a new line"
	)

	state := stWaitingPkg
	pkg := ""
	ver := ""
	iid := ""
	lineNo := 0
	makeError := func(fmtStr string, args ...interface{}) error {
		args = append([]interface{}{lineNo}, args...)
		return fmt.Errorf("failed to parse versions file (line %d): "+fmtStr, args...)
	}
	output := make([]PackageInstance, 0)
	for versionScanner.Scan() {
		lineNo++
		line := strings.TrimSpace(versionScanner.Text())
		// Comments are grammatically insignificant (unlike empty lines), so skip
		// the completely.
		if len(line) > 0 && line[0] == '#' {
			continue
		}

		switch state {
		case stWaitingPkg:
			if line == "" {
				continue // can have more than one empty line between triples
			}
			pkg = line
			state = stWaitingVer

		case stWaitingVer:
			if line == "" {
				return nil, makeError("expecting a version name, not a new line")
			}
			ver = line
			state = stWaitingIID

		case stWaitingIID:
			if line == "" {
				return nil, makeError("expecting an instance ID, not a new line")
			}
			iid = line
			output = append(output, PackageInstance{pkg, ver, iid})
			pkg, ver, iid = "", "", ""
			state = stWaitingNL

		case stWaitingNL:
			if line == "" {
				state = stWaitingPkg
				continue
			}
			return nil, makeError("expecting an empty line between each version definition triple")
		}
	}
	return output, nil
}

// Platform contains the parameters for a "${platform}" template.
// The string value can be obtained by calling String().
type Platform struct {
	// OS defines the operating system of this platform. It can be any OS
	// supported by golang.
	OS string
	// Arch defines the CPU architecture of this platform. It can be any
	// architecture supported by golang.
	Arch string
}

// String generates a string represents the Platform in "OS"-"Arch" form.
func (p Platform) String() string {
	return p.OS + "-" + p.Arch
}

// Expander returns an Expander populated with p's fields.
func (p Platform) Expander() Expander {
	return Expander{
		"os":       p.OS,
		"arch":     p.Arch,
		"platform": p.String(),
	}
}

// Expander is a mapping of simple string substitutions which is used to
// expand cipd package name templates. For example:
//
//   ex, err := template.Expander{
//     "platform": "mac-amd64"
//   }.Expand("foo/${platform}")
//
// `ex` would be "foo/mac-amd64".
type Expander map[string]string

// Expand applies package template expansion rules to the package template,
//
// If err == ErrSkipTemplate, that means that this template does not apply to
// this os/arch combination and should be skipped.
//
// The expansion rules are as follows:
//   - "some text" will pass through unchanged
//   - "${variable}" will directly substitute the given variable
//   - "${variable=val1,val2}" will substitute the given variable, if its value
//     matches one of the values in the list of values. If the current value
//     does not match, this returns ErrSkipTemplate.
//
// Attempting to expand an unknown variable is an error.
// After expansion, any lingering '$' in the template is an error.
func (t Expander) Expand(template string) (pkg string, err error) {
	skip := false

	pkg = templateRE.ReplaceAllStringFunc(template, func(parm string) string {
		// ${...}
		contents := parm[2 : len(parm)-1]

		varNameValues := strings.SplitN(contents, "=", 2)
		if len(varNameValues) == 1 {
			// ${varName}
			if value, ok := t[varNameValues[0]]; ok {
				return value
			}

			err = fmt.Errorf("unknown variable in ${%s}", contents)
		}

		// ${varName=value,value}
		ourValue, ok := t[varNameValues[0]]
		if !ok {
			err = fmt.Errorf("unknown variable %q", parm)
			return parm
		}

		for _, val := range strings.Split(varNameValues[1], ",") {
			if val == ourValue {
				return ourValue
			}
		}
		skip = true
		return parm
	})
	if skip {
		err = ErrSkipTemplate
	}
	if err == nil && strings.ContainsRune(pkg, '$') {
		err = fmt.Errorf("unable to process some variables in %q", template)
	}
	return
}

// Expand method expands a cipdPath that contains templates such as ${platform}
// into concrete full paths.
func Expand(cipdPath string, platforms []Platform) ([]string, error) {
	output := make([]string, 0)
	//expanders := make([]Expander, 0)
	if !templateRE.MatchString(cipdPath) {
		output = append(output, cipdPath)
		return output, nil
	}

	for _, plat := range platforms {
		pkg, err := plat.Expander().Expand(cipdPath)
		if err == ErrSkipTemplate {
			continue
		}
		if err != nil {
			return nil, err
		}
		output = append(output, pkg)
	}
	return output, nil
}

// DefaultPlatforms returns a slice of Platform objects that are currently
// validated by jiri.
func DefaultPlatforms() []Platform {
	return []Platform{
		Platform{"linux", "amd64"},
		Platform{"mac", "amd64"},
	}
}
