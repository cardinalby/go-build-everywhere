package xgolib

type BuildArgs struct {
	// Print the names of packages as they are compiled (flag: v)
	Verbose bool
	// Print the command as executing the builds (flag: x)
	Steps bool
	// Enable data race detection (supported only on amd64) (flag: race)
	Race bool
	// List of build tags to consider satisfied during the build (flag: tags)
	Tags string
	// Arguments to pass on each go tool link invocation (flag: ldflags)
	LdFlags string
	// Indicates which kind of object file to build (flag: buildmode)
	Mode string
	// Whether to stamp binaries with version control information (flag: buildvcs)
	VCS string
	// Remove all file system paths from the resulting executable (flag: trimpath)
	TrimPath bool
}

func (args *BuildArgs) SetDefaults() {
	if args.Mode == "" {
		args.Mode = "default"
	}
}

type Args struct {
	// Repository is root import path to build (command line arg):
	Repository string
	// Go release to use for cross compilation (flag: go)
	GoVersion string
	// Set a Global Proxy for Go Modules (flag: goproxy)
	GoProxy string
	// Sub-package to build if not root import (flag: pkg)
	SrcPackage string
	// Version control remote repository to build (flag: remote)
	SrcRemote string
	// Version control branch to build (flag: branch)
	SrcBranch string
	// Prefix to use for output naming (empty = package name) (flag: out)
	OutPrefix string
	// Destination folder to put binaries in (empty = current) (flag: dest)
	OutFolder string
	// CGO dependencies (configure/make based archives) (flag: deps)
	CrossDeps string
	// CGO dependency configure arguments (flag: depsargs)
	CrossArgs string
	// Targets to build for (flag: targets)
	Targets []string
	// Use custom docker repo instead of official distribution (flag: docker-repo)
	DockerRepo string
	// Use custom docker image instead of official distribution (flag: docker-image)
	DockerImage string
	// Arguments of go build command (flag: build)
	Build BuildArgs
}

func (a *Args) SetDefaults() {
	if a.GoVersion == "" {
		a.GoVersion = "latest"
	}
	if len(a.Targets) == 0 {
		a.Targets = []string{"*/*"}
	}
	if a.GoProxy == "" {
		a.GoProxy = "https://proxy.golang.org,direct"
	}
	a.Build.SetDefaults()
}
