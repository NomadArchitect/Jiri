# Jiri
/jɪəri/ YEER-ee

*"Jiri integrates repositories intelligently"*

Jiri is a tool for multi-repo development.
It supports:
* syncing multiple local GIT repos with upstream,
* capturing the current state of all local repos in a "snapshot",
* restoring local project state from a snapshot, and
* facilitating sending change lists to [Gerrit][gerrit].

Jiri has an extensible plugin model, making it easy to create new sub-commands.

Jiri is open-source.

## Manually build jiri
We have [prebuilts](#Bootstrapping) for linux and darwin `x86_64` systems. In
order to fetch latest jiri source code and build jiri manually, latest version
of [Go](https://golang.org) should be installed. After installing Go, you can
fetch the latest jiri source code by using the command:

```
git clone https://fuchsia.googlesource.com/jiri
```

To build (or rebuild) jiri, simply type the following commands:

```
cd jiri
go install ./cmd/jiri
```

The binary will be installed to `$HOME/go/bin/jiri` (or `$GOPATH/bin/jiri`, if
you set `GOPATH`) and can be copied to any directory in your PATH, as long as it
is writable (to support jiri bootstrapping and self-updates).

## Uploading a change to the jiri repository

To upload a change to Jiri itself, commit your changes locally
and run:

```
git push origin HEAD:refs/for/main
```

The first time you run this command you will see instructions for
installing and running a git commit-hook.

Running the above command again will print out the URL for your Changelist.

## Jiri Behaviour
[See this][behaviour]

## Jiri Tricks
[See this][how do i]

## Jiri Basics
Jiri organizes a set of GIT repositories on your local filesystem according to
a [manifest][manifests].  These repositories are referred to as "projects", and
are all contained within a single directory called the "jiri root".

Jiri also supports [CIPD][cipd] "packages", to download potentially large
_read-only_ files, like toolchain binaries or test data, into the jiri root.

The manifest file specifies the relative location of each project or package
within the jiri root, and also includes other metadata, such as its remote url,
the remote branch or revision it should track, and more.

The `jiri update` command syncs the main branch of all local projects to the
revision and remote branch specified in the manifest for each project. Jiri
will create the project locally if it does not exist, and if run with the `-gc`
flag, jiri will "garbage collect" any projects that are not listed in the
manifest by deleting them locally.

The command will also download, update or remove CIPD packages according to
manifest changes, if necessary.

The `.jiri_manifest` file in the jiri root describes which project jiri should
sync.  Typically the `.jiri_manifest` file will import other manifests, but it
can also contain a list of projects.

For example, here is a simple `.jiri_manifest` with just two projects, "foo"
and "bar", which are hosted on github and bitbucket respectively.
```
<?xml version="1.0" encoding="UTF-8"?>
<manifest>
  <projects>
    <project name="foo-project"
             remote="https://github.com/my-org/foo"
             path="foo"/>
    <project name="bar"
             remote="https://bitbucket.com/other-org/bar"
             path="bar"/>
  </projects>
</manifest>
```
When you run `jiri update` for the first time, the "foo" and "bar" repos will
be cloned into `foo` and `bar` respectively and repos would be put on DETACHED
HEAD.  Running `jiri update` again will update all the remote refs and
rebase your current branch to its upstream branch.

Note that the project paths do not need to be immediate children of the jiri
root.  We could have decided to set the `path` attribute for the "bar" project
to "third_party/bar", or even nest "bar" inside the "foo" project by setting
the `path` to  "foo/bar" (assuming no files in the foo repo conflict with bar).

Because manifest files also need to be kept in sync between various team
members, it often makes sense to keep your team's manifests in a version
controlled repository.

Jiri makes it easy to "import" a remote manifest from your local
`.jiri_manifest` file with the `jiri import` command.  For example, running the
following command will create a `.jiri_manifest` file (or append to an existing
one) with an `import` tag that imports the jiri manifest from the
`https://fuchsia.googlesource.com/jiri` repo.

```
jiri import -name jiri manifest https://fuchsia.googlesource.com/jiri
```

The next time you run `jiri update`, jiri will sync all projects listed in the
jiri manifest.

## Quickstart

This section explains how to get started with jiri.

First we "bootstrap" jiri so that it can sync and build itself.

Then we create and import a new manifest, which specifies how jiri should
manage your projects.

### Bootstrapping

You can get jiri up-and-running in no time with the help of the [bootstrap
script][bootstrap_jiri].

First, pick a jiri root directory.  All projects will be synced to
subdirectories of the root.

```
export MY_ROOT=$HOME/myroot
```

Execute the `jiri_bootstrap` script, which will fetch and build the jiri tool,
and initialize the root directory.

```
curl -s https://fuchsia.googlesource.com/jiri/+/HEAD/scripts/bootstrap_jiri?format=TEXT | base64 --decode | bash -s "$MY_ROOT"
```

The `jiri` command line tool will be installed in
`$MY_ROOT/.jiri_root/bin/jiri`, so add that to your `PATH`.

```
export PATH="$MY_ROOT"/.jiri_root/bin:$PATH
```

Next, use the `jiri import` command to import the "jiri" manifest from the
Jiri repo.  This manifest includes Jiri's repository.

You can see the jiri manifest [here][jiri manifest]. For more
information on manifests, read the [manifest docs][manifests].

```
cd "$MY_ROOT"
jiri import -name jiri manifest https://fuchsia.googlesource.com/jiri
```

You should now have a file in the root directory called `.jiri_manifest`, which
will contain a single import.

Finally, run `jiri update`, which will sync all local projects to the revisions
listed in the manifest (which in this case will be `HEAD`).

```
jiri update
```

You should now see the imported project in `$MY_ROOT/go/src/fuchsia.googlesource.com/jiri`.

Running `jiri update` again will sync the local repos to the remotes, and
update the jiri tool.

### Managing your projects with jiri

Now that jiri is able to sync and build itself, we must tell it how to manage
your projects.

In order for jiri to manage a set of projects, those projects must be listed in
a [manifest][manifests], and that manifest must be hosted in a git repo.

If you already have a manifest hosted in a git repo, you can import that
manifest the same way we imported the "jiri" manifest.

For example, if your manifest is called "my_manifest" and is in a repo hosted
at "https://github.com/my_org/manifests", then you can import that manifest
as follows.

```
jiri import my_manifest https://github.com/my_org/manifests
```

The rest of this section walks through how to create a manifest from scratch,
host it from a local git repo, and get jiri to manage it.

Suppose that the project you want jiri to manage is the "Hello-World" repo
located at https://github.com/Test-Octowin/Hello-World.

First we'll create a new git repo to host the manifest we'll be writing.

```
mkdir -p /tmp/my_manifest_repo
cd /tmp/my_manifest_repo
git init
```

Next we'll create a manifest and commit it to the manifest repo.

The manifest file will include the Hello-World repo as well as the manifest
repo itself.

```
cat <<EOF > my_manifest
<?xml version="1.0" encoding="UTF-8"?>
<manifest>
  <projects>
    <project name="Hello-World"
             remote="https://github.com/Test-Octowin/Hello-World"
             path="helloworld"/>
    <project name="my_manifest_repo"
             remote="/tmp/my_manifest_repo"
             path="my_manifest_repo"/>
  </projects>
</manifest>
EOF

git add my_manifest
git commit -m "Add my_manifest."
```

This manifest contains a single project with the name "Hello-World" and the
remote of the repo.  The `path` attribute tells jiri to sync this repo inside
the `helloworld` directory.

Normally we would want to push this repo to some remote to make it accessible
to other users who want to sync the same projects.  For now, however, we'll
just refer to the repo by its path in the local filesystem.

Now we just need to import that new manifest and `jiri update`.

```
cd "$MY_ROOT"
jiri import -name=my_manifest_repo my_manifest /tmp/my_manifest_repo
jiri update
```

You should now see the Hello-World repo in `$MY_ROOT/helloworld`, and your
manifest repo in `$MY_ROOT/my_manifest_repo`.

## Command-line help

The `jiri help` command will print help documentation about the `jiri` tool and
its subcommands.

For general documentation, including a list of subcommands, run `jiri help`.
To find documentation about a specific topic or subcommand, run `jiri help
<command>`.

### Main commands are:
```
   branch          Show or delete branches
   diff            Prints diff between two snapshots
   grep            Search across projects.
   import          Adds imports to .jiri_manifest file
   init            Create a new jiri root
   patch           Patch in the existing change
   project         Manage the jiri projects
   project-config  Prints/sets project's local config
   run-hooks       Run hooks using local manifest
   runp            Run a command in parallel across jiri projects
   selfupdate      Update jiri tool
   snapshot        Create a new project snapshot
   source-manifest Create a new source-manifest from current checkout
   status          Prints status of all the projects
   update          Update all jiri projects
   upload          Upload a changelist for review
   version         Print the jiri version
   help            Display help for commands or topics
```
Run `jiri help [command]` for command usage.

## Filesystem

See the jiri [filesystem docs][filesystem doc].

## Manifests<a name="manifests"></a>

See the jiri [manifest docs][manifest doc].

## Snapshots

TODO(anmittal): Write me.

## Gerrit CL workflow

[Gerrit][gerrit] is a collaborative code-review tool used by many open source
projects.

One of the peculiarities of Gerrit is that it expects a changelist to be
represented by a single commit.  This constrains the way developers may use git
to work on their changes.  In particular, they must use the --amend flag with
all but the first git commit operation and they need to use git rebase to sync
their pending code change with the remote.  See Android's [repo command
reference][android repo] or Go's [contributing instructions][go contrib] for
examples of how intricate the workflow for resolving conflicts between the
pending code change and the remote is.

The rest of this section describes common development operations using `jiri
upload`.

### Using feature branches

All development should take place on a non-main "feature" branch.  Once the
code is reviewed and approved, it is merged into the remote main branch via the
Gerrit code review system.  The change can then be merged into the local
branches with `jiri update -rebase-all`.

### Creating a new CL

1. Sync the main branch with the remote.
  ```
  jiri update
  ```
2. Create a new feature branch for the CL.
  ```
  git checkout -b <branch-name> --track origin/main
  ```
3. Make modifications to the project source code.
4. Stage any changed files for commit.
  ```
  git add <file1> <file2> ... <fileN>
  ```
5. Commit the changes.
  ```
  git commit
  ```

### Syncing a CL with the remote

1. Sync the main branch with the remote.
  ```
  jiri update
  ```
2. Switch to the feature branch that corresponds to the CL under development.
  ```
  git checkout <branch-name>
  ```
3. Sync the feature branch with the main branch.
  ```
  git rebase origin/main
  ```
4. If there are no conflicts between the main branch and the feature branch, the CL
   has been successfully synced with the remote.
5. If there are conflicts:
  1. Manually [resolve the conflicts][github resolve conflict].
  2. Stage any changed files for a commit.
    ```
    git add <file1> <file2> ... <fileN>
    ```
  3. Commit the changes.
    ```
    git commit --amend
    ```

### Requesting a code review

1. Switch to the feature branch that corresponds to the CL under development.
  ```
  git checkout <branch-name>
  ```
2.  Upload the CL to Gerrit.
  ```
  jiri upload
  ```

If the CL upload is  successful, this will print the URL of the CL hosted on
Gerrit.  You can add reviewers and comments through the [Gerrit web UI][gerrit
web ui] at that URL.

Note that there are many useful flags for `jiri upload`.  You can learn about them
by running `jiri help upload`.

### Reviewing a CL

1. Follow the link received in the code review email request.
2. Use the [Gerrit web UI][gerrit web UI] to comment on the CL and click the
   "Reply" button to submit comments, selecting the appropriate code-review
   score.

### Addressing review comments


1. Switch to the feature branch that corresponds to the CL under development.
  ```
  git checkout <branch-name>
  ```
2. Modify the code.
3. Add your modified code.
  ```
  git add -u
  ```
4. Commit the code.
  ```
  git commit --amend
  ```
5. Reply to each Gerrit comment and click the "Reply" button to send them.
6. Send the updated CL to Gerrit.
  ```
  jiri upload
  ```

### Submitting a CL
1. Note that if the CL conflicts with any changes that have been submitted since
   the last update of the CL, these conflicts need to be resolved before the CL
   can be submitted.  To do so, rebase your changes then upload the updated CL
   to Gerrit.
  ```
  jiri upload
  ```
2. Once a CL meets the conditions for being submitted, it can be merged into
   the remote main branch by clicking the "Submit" button on the Gerrit web
   UI.
3. Delete the local feature branch after the CL has been submitted to Gerrit.
  1. Sync the main branch to the laster version of the remote.
    ```
    jiri update
    ```
  2. Safely delete the feature branch that corresponds to the CL.
    ```
    git checkout JIRI_HEAD && git branch -d <branch-name>
    ```

### Dependent CLs
If you have changes A and B, and B depends on A, you can still submit distinct
CLs for A and B that can be reviewed and submitted independently (although A
must be submitted before B).

First, create your feature branch for A, make your change, and upload the CL
for review according to the instructions above.

Then, while still on the feature branch for A, create your feature branch for B.
```
git checkout -b feature-B --track origin/main
```
Then make your change and upload the CL for review according to the
instructions above.

You can respond to review comments by submitting new patch sets as normal.

After the CL for A has been submitted, make sure to clean up A's feature branch
and upload a new patch set for feature B.
```
jiri update # fetch update that includes feature A
git checkout feature-B
git rebase -i origin/main # if u see commit from A, delete it and then rebase
properly
jiri upload # send new patch set for feature B
```
The CL for feature B can now be submitted.

This process can be extended for more than 2 CLs.  You must keep two things in mind:
* always create the dependent feature branch from the parent feature branch, and
* after a parent feature has been submitted, rebase feature-B onto origin/main

## FAQ

### Why the name "jiri"?
The tool was conceived by engineers working on the [Vanadium][vanadium] project
to facilitate the multi-repository management needs of the project. At the time,
it was called "v23". It was renamed to "jiri" shortly after its creator (named
Jiří) left the project and Google.

[Jiří][jiri-wiki] is a very popular boys name in the Czech Republic.

### How do you pronounce "jiri"?
We pronounce "jiri" like "yiree".

The actual Czech name [Jiří][jiri-wiki] is pronounced something like "yirzhee".

### How can I test changes to a manifest without pushing it upstream?
see [Jiri local update][hacking doc]

[android repo]: https://source.android.com/source/using-repo.html "Repo command reference"
[bootstrap_jiri]: scripts/bootstrap_jiri "bootstrap_jiri"
[gerrit]: https://www.gerritcodereview.com/ "Gerrit code review"
[gerrit web ui]: https://gerrit-review.googlesource.com/Documentation/user-review-ui.html "Gerrit review UI"
[github resolve conflict]: https://help.github.com/articles/resolving-a-merge-conflict-from-the-command-line/ "Resolving a merge conflict"
[go contrib]: https://golang.org/doc/contribute.html#Code_review "Go Contribution Guidelines - Code Review"
[jiri-wiki]: https://en.wikipedia.org/wiki/Ji%C5%99%C3%AD "Jiří"
[manifests]: #manifests "manifests"
[jiri manifest]: https://fuchsia.googlesource.com/jiri/+/refs/heads/main/manifest "jiri manifest"
[manifest doc]:/manifest.md "Jiri manifest"
[filesystem doc]:/filesystem.md "Jiri filesystem"
[hacking doc]:/HACKING.md "Jiri local updates"
[behaviour]:/behaviour.md
[how do i]:/howdoi.md
[vanadium]: https://v.io/
[cipd]: https://chromium.googlesource.com/infra/luci/luci-go/+/main/cipd/
