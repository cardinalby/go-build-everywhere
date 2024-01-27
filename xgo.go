package xgolib

import (
	"fmt"
	"go/build"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var version = "dev"
var depsCache = filepath.Join(os.TempDir(), "xgo-cache")

// Cross compilation docker containers
var dockerDist = "ghcr.io/crazy-max/xgo"

// ConfigFlags is a simple set of flags to define the environment and dependencies.
type ConfigFlags struct {
	Repository   string   // Root import path to build
	Package      string   // Sub-package to build if not root import
	Prefix       string   // Prefix to use for output naming
	Remote       string   // Version control remote repository to build
	Branch       string   // Version control branch to build
	Dependencies string   // CGO dependencies (configure/make based archives)
	Arguments    string   // CGO dependency configure arguments
	Targets      []string // Targets to build for
	GoProxy      string   // Set a Global Proxy for Go Modules
}

// BuildFlags is a simple collection of flags to fine tune a build.
type BuildFlags struct {
	Verbose  bool   // Print the names of packages as they are compiled
	Steps    bool   // Print the command as executing the builds
	Race     bool   // Enable data race detection (supported only on amd64)
	Tags     string // List of build tags to consider satisfied during the build
	LdFlags  string // Arguments to pass on each go tool link invocation
	Mode     string // Indicates which kind of object file to build
	VCS      string // Whether to stamp binaries with version control information
	TrimPath bool   // Remove all file system paths from the resulting executable
}

func StartBuild(args Args, logger *log.Logger) error {
	args.SetDefaults()
	defer logger.Println("INFO: Completed!")
	logger.Printf("INFO: Starting xgo/%s", version)

	xgoInXgo := os.Getenv("XGO_IN_XGO") == "1"
	if xgoInXgo {
		depsCache = "/deps-cache"
	}
	// Only use docker images if we're not already inside out own image
	image := ""

	if !xgoInXgo {
		// Ensure docker is available
		if err := checkDocker(logger); err != nil {
			return fmt.Errorf("failed to check docker installation: %w", err)
		}
		// Validate the command line arguments
		if args.Repository == "" {
			return fmt.Errorf("go import path is not set")
		}
		// Select the image to use, either official or custom
		image = fmt.Sprintf("%s:%s", dockerDist, args.GoVersion)
		if args.DockerImage != "" {
			image = args.DockerImage
		} else if args.DockerRepo != "" {
			image = fmt.Sprintf("%s:%s", args.DockerRepo, args.GoVersion)
		}
		// Check that all required images are available
		found := checkDockerImage(image, logger)
		switch {
		case !found:
			logger.Println("not found!")
			if err := pullDockerImage(image, logger); err != nil {
				return fmt.Errorf("failed to pull docker image from the registry: %w", err)
			}
		default:
			logger.Println("INFO: Docker image found!")
		}
	}
	// Cache all external dependencies to prevent always hitting the internet
	if args.CrossDeps != "" {
		if err := os.MkdirAll(depsCache, 0751); err != nil {
			return fmt.Errorf("failed to create dependency cache: %w", err)
		}
		// Download all missing dependencies
		for _, dep := range strings.Split(args.CrossDeps, " ") {
			if url := strings.TrimSpace(dep); len(url) > 0 {
				path := filepath.Join(depsCache, filepath.Base(url))

				if _, err := os.Stat(path); err != nil {
					logger.Printf("INFO: Downloading new dependency: %s...", url)
					out, err := os.Create(path)
					if err != nil {
						return fmt.Errorf("failed to create dependency file: %w", err)
					}
					res, err := http.Get(url)
					if err != nil {
						return fmt.Errorf("failed to retrieve dependency: %w", err)
					}
					if err := func() error {
						defer func() {
							if err := res.Body.Close(); err != nil {
								logger.Printf("ERROR: Failed to close response body: %v", err)
							}
						}()

						if _, err := io.Copy(out, res.Body); err != nil {
							return fmt.Errorf("INFO: Failed to download dependency: %v", err)
						}
						return out.Close()
					}(); err != nil {
						return err
					}
					logger.Printf("INFO: New dependency cached: %s.", path)
				} else {
					fmt.Printf("INFO: Dependency already cached: %s.", path)
				}
			}
		}
	}
	// Assemble the cross compilation environment and build options
	config := &ConfigFlags{
		Repository:   args.Repository,
		Package:      args.SrcPackage,
		Remote:       args.SrcRemote,
		Branch:       args.SrcBranch,
		Prefix:       args.OutPrefix,
		Dependencies: args.CrossDeps,
		Arguments:    args.CrossArgs,
		Targets:      args.Targets,
		GoProxy:      args.GoProxy,
	}
	logger.Printf("DBG: config: %+v", config)
	flags := &BuildFlags{
		Verbose:  args.Build.Verbose,
		Steps:    args.Build.Steps,
		Race:     args.Build.Race,
		Tags:     args.Build.Tags,
		LdFlags:  args.Build.LdFlags,
		Mode:     args.Build.Mode,
		VCS:      args.Build.VCS,
		TrimPath: args.Build.TrimPath,
	}
	logger.Printf("DBG: flags: %+v", flags)
	folder, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to retrieve the working directory: %w", err)
	}
	if args.OutFolder != "" {
		folder, err = filepath.Abs(args.OutFolder)
		if err != nil {
			return fmt.Errorf("failed to resolve destination path (%s): %w", args.OutFolder, err)
		}
	}
	// Execute the cross compilation, either in a container or the current system
	if !xgoInXgo {
		err = compile(image, config, flags, folder, logger)
	} else {
		err = compileContained(config, flags, folder, logger)
	}
	if err != nil {
		return fmt.Errorf("failed to cross compile package: %w", err)
	}
	return nil
}

// Checks whether a docker installation can be found and is functional.
func checkDocker(logger *log.Logger) error {
	logger.Println("INFO: Checking docker installation...")
	if err := run(exec.Command("docker", "version")); err != nil {
		return err
	}
	logger.Println("")
	return nil
}

// Checks whether a required docker image is available locally.
func checkDockerImage(image string, logger *log.Logger) bool {
	logger.Printf("INFO: Checking for required docker image %s... ", image)
	err := exec.Command("docker", "image", "inspect", image).Run()
	return err == nil
}

// Pulls an image from the docker registry.
func pullDockerImage(image string, logger *log.Logger) error {
	logger.Printf("INFO: Pulling %s from docker registry...", image)
	return run(exec.Command("docker", "pull", image))
}

// compile cross builds a requested package according to the given build specs
// using a specific docker cross compilation image.
func compile(
	image string,
	config *ConfigFlags,
	flags *BuildFlags,
	folder string,
	logger *log.Logger,
) error {
	// If a local build was requested, find the import path and mount all GOPATH sources
	var locals, mounts, paths []string
	var usesModules bool
	if strings.HasPrefix(config.Repository, string(filepath.Separator)) || strings.HasPrefix(config.Repository, ".") {
		if fileExists(filepath.Join(config.Repository, "go.mod")) {
			usesModules = true
		}
		if !usesModules {
			// Resolve the repository import path from the file path
			if repository, err := resolveImportPath(config.Repository, logger); err != nil {
				return err
			} else {
				config.Repository = repository
			}
			if fileExists(filepath.Join(config.Repository, "go.mod")) {
				usesModules = true
			}
		}
		if !usesModules {
			logger.Println("INFO: go.mod not found. Skipping go modules")
		}

		gopathEnv := os.Getenv("GOPATH")
		if gopathEnv == "" && !usesModules {
			logger.Printf("INFO: No $GOPATH is set - defaulting to %s", build.Default.GOPATH)
			gopathEnv = build.Default.GOPATH
		}

		// Iterate over all the local libs and export the mount points
		if gopathEnv == "" && !usesModules {
			return fmt.Errorf("INFO: No $GOPATH is set or forwarded to xgo")
		}

		if !usesModules {
			if err := os.Setenv("GO111MODULE", "off"); err != nil {
				return err
			}
			for _, gopath := range strings.Split(gopathEnv, string(os.PathListSeparator)) {
				// Since docker sandboxes volumes, resolve any symlinks manually
				sources := filepath.Join(gopath, "src")
				if err := filepath.Walk(sources, func(path string, info os.FileInfo, err error) error {
					// Skip any folders that errored out
					if err != nil {
						logger.Printf("WARNING: Failed to access GOPATH element %s: %v", path, err)
						return nil
					}
					// Skip anything that's not a symlink
					if info.Mode()&os.ModeSymlink == 0 {
						return nil
					}
					// Resolve the symlink and skip if it's not a folder
					target, err := filepath.EvalSymlinks(path)
					if err != nil {
						return nil
					}
					if info, err = os.Stat(target); err != nil || !info.IsDir() {
						return nil
					}
					// Skip if the symlink points within GOPATH
					if filepath.HasPrefix(target, sources) {
						return nil
					}

					// Folder needs explicit mounting due to docker symlink security
					locals = append(locals, target)
					mounts = append(mounts, filepath.Join("/ext-go", strconv.Itoa(len(locals)), "src", strings.TrimPrefix(path, sources)))
					paths = append(paths, filepath.ToSlash(filepath.Join("/ext-go", strconv.Itoa(len(locals)))))
					return nil
				}); err != nil {
					return err
				}

				// Export the main mount point for this GOPATH entry
				locals = append(locals, sources)
				mounts = append(mounts, filepath.Join("/ext-go", strconv.Itoa(len(locals)), "src"))
				paths = append(paths, filepath.ToSlash(filepath.Join("/ext-go", strconv.Itoa(len(locals)))))
			}
		}
	}
	// Assemble and run the cross compilation command
	logger.Printf("INFO: Cross compiling %s package...", config.Repository)

	args := []string{
		"run", "--rm",
		"-v", folder + ":/build",
		"-v", depsCache + ":/deps-cache:ro",
		"-e", "REPO_REMOTE=" + config.Remote,
		"-e", "REPO_BRANCH=" + config.Branch,
		"-e", "PACK=" + config.Package,
		"-e", "DEPS=" + config.Dependencies,
		"-e", "ARGS=" + config.Arguments,
		"-e", "OUT=" + config.Prefix,
		"-e", fmt.Sprintf("FLAG_V=%v", flags.Verbose),
		"-e", fmt.Sprintf("FLAG_X=%v", flags.Steps),
		"-e", fmt.Sprintf("FLAG_RACE=%v", flags.Race),
		"-e", fmt.Sprintf("FLAG_TAGS=%s", flags.Tags),
		"-e", fmt.Sprintf("FLAG_LDFLAGS=%s", flags.LdFlags),
		"-e", fmt.Sprintf("FLAG_BUILDMODE=%s", flags.Mode),
		"-e", fmt.Sprintf("FLAG_BUILDVCS=%s", flags.VCS),
		"-e", fmt.Sprintf("FLAG_TRIMPATH=%v", flags.TrimPath),
		"-e", "TARGETS=" + strings.Replace(strings.Join(config.Targets, " "), "*", ".", -1),
	}
	if usesModules {
		args = append(args, []string{"-e", "GO111MODULE=on"}...)
		args = append(args, []string{"-v", build.Default.GOPATH + ":/go"}...)
		if config.GoProxy != "" {
			args = append(args, []string{"-e", fmt.Sprintf("GOPROXY=%s", config.GoProxy)}...)
		}

		// Map this repository to the /source folder
		absRepository, err := filepath.Abs(config.Repository)
		if err != nil {
			return fmt.Errorf("failed to locate requested module repository: %w", err)
		}
		args = append(args, []string{"-v", absRepository + ":/source"}...)

		// Check whether it has a vendor folder, and if so, use it
		vendorPath := absRepository + "/vendor"
		vendorfolder, err := os.Stat(vendorPath)
		if !os.IsNotExist(err) && vendorfolder.Mode().IsDir() {
			args = append(args, []string{"-e", "FLAG_MOD=vendor"}...)
			logger.Printf("INFO: Using vendored Go module dependencies")
		}
	} else {
		args = append(args, []string{"-e", "GO111MODULE=off"}...)
		for i := 0; i < len(locals); i++ {
			args = append(args, []string{"-v", fmt.Sprintf("%s:%s:ro", locals[i], mounts[i])}...)
		}
		args = append(args, []string{"-e", "EXT_GOPATH=" + strings.Join(paths, ":")}...)
	}

	args = append(args, []string{image, config.Repository}...)
	logger.Printf("INFO: Docker %s", strings.Join(args, " "))
	return run(exec.Command("docker", args...))
}

// compileContained cross builds a requested package according to the given build
// specs using the current system opposed to running in a container. This is meant
// to be used for cross compilation already from within an xgo image, allowing the
// inheritance and bundling of the root xgo images.
func compileContained(config *ConfigFlags, flags *BuildFlags, folder string, logger *log.Logger) error {
	// If a local build was requested, resolve the import path
	local := strings.HasPrefix(config.Repository, string(filepath.Separator)) || strings.HasPrefix(config.Repository, ".")
	if local {
		// Resolve the repository import path from the file path
		if repository, err := resolveImportPath(config.Repository, logger); err != nil {
			return err
		} else {
			config.Repository = repository
		}

		// Determine if this is a module-based repository
		usesModules := fileExists(filepath.Join(config.Repository, "go.mod"))
		if !usesModules {
			if err := os.Setenv("GO111MODULE", "off"); err != nil {
				return err
			}
			logger.Println("INFO: Don't use go modules (go.mod not found)")
		}
	}
	// Fine tune the original environment variables with those required by the build script
	env := []string{
		"REPO_REMOTE=" + config.Remote,
		"REPO_BRANCH=" + config.Branch,
		"PACK=" + config.Package,
		"DEPS=" + config.Dependencies,
		"ARGS=" + config.Arguments,
		"OUT=" + config.Prefix,
		fmt.Sprintf("FLAG_V=%v", flags.Verbose),
		fmt.Sprintf("FLAG_X=%v", flags.Steps),
		fmt.Sprintf("FLAG_RACE=%v", flags.Race),
		fmt.Sprintf("FLAG_TAGS=%s", flags.Tags),
		fmt.Sprintf("FLAG_LDFLAGS=%s", flags.LdFlags),
		fmt.Sprintf("FLAG_BUILDMODE=%s", flags.Mode),
		fmt.Sprintf("FLAG_BUILDVCS=%s", flags.VCS),
		fmt.Sprintf("FLAG_TRIMPATH=%v", flags.TrimPath),
		"TARGETS=" + strings.Replace(strings.Join(config.Targets, " "), "*", ".", -1),
	}
	if local {
		env = append(env, "EXT_GOPATH=/non-existent-path-to-signal-local-build")
	}
	// Assemble and run the local cross compilation command
	logger.Printf("INFO: Cross compiling %s package...", config.Repository)

	cmd := exec.Command("xgo-build", config.Repository)
	cmd.Env = append(os.Environ(), env...)

	return run(cmd)
}

// resolveImportPath converts a package given by a relative path to a Go import
// path using the local GOPATH environment.
func resolveImportPath(path string, logger *log.Logger) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to locate requested package: %w", err)
	}
	stat, err := os.Stat(abs)
	if err != nil || !stat.IsDir() {
		return "", fmt.Errorf("requested path invalid")
	}
	pack, err := build.ImportDir(abs, build.FindOnly)
	if err != nil {
		return "", fmt.Errorf("failed to resolve import path: %w", err)
	}
	return pack.ImportPath, nil
}

// Executes a command synchronously, redirecting its output to stdout.
func run(cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// fileExists checks if given file exists
func fileExists(file string) bool {
	if _, err := os.Stat(file); os.IsNotExist(err) {
		return false
	}
	return true
}
