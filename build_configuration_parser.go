package gobuild

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mattn/go-shellwords"
)

//go:generate faux --interface TargetManager --output fakes/target_manager.go
type TargetManager interface {
	CleanAndValidate(targets []string, workingDir string) ([]string, error)
	GenerateDefaults(workingDir string) ([]string, error)
}

//go:generate faux --interface BuildpackYMLParser --output fakes/buildpack_yml_parser.go
type BuildpackYMLParser interface {
	Parse(buildpackVersion, workingDir string) (BuildConfiguration, error)
}

type BuildConfiguration struct {
	Targets     []string
	Flags       []string
	ImportPath  string
	GenerateBOM bool
}

type BuildConfigurationParser struct {
	targetManager      TargetManager
	buildpackYMLParser BuildpackYMLParser
}

func NewBuildConfigurationParser(targetManager TargetManager, buildpackYMLParser BuildpackYMLParser) BuildConfigurationParser {
	return BuildConfigurationParser{
		targetManager:      targetManager,
		buildpackYMLParser: buildpackYMLParser,
	}
}

func (p BuildConfigurationParser) Parse(buildpackVersion, workingDir string) (BuildConfiguration, error) {
	var buildConfiguration BuildConfiguration

	_, err := os.Stat(filepath.Join(workingDir, "buildpack.yml"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return BuildConfiguration{}, err
		}
	} else {
		buildConfiguration, err = p.buildpackYMLParser.Parse(buildpackVersion, workingDir)
		if err != nil {
			return BuildConfiguration{}, err
		}
	}

	if val, ok := os.LookupEnv("BP_GO_TARGETS"); ok {
		buildConfiguration.Targets = filepath.SplitList(val)
	}

	if len(buildConfiguration.Targets) > 0 {
		buildConfiguration.Targets, err = p.targetManager.CleanAndValidate(buildConfiguration.Targets, workingDir)
		if err != nil {
			return BuildConfiguration{}, err
		}
	} else {
		buildConfiguration.Targets, err = p.targetManager.GenerateDefaults(workingDir)
		if err != nil {
			return BuildConfiguration{}, err
		}
	}

	buildConfiguration.Flags, err = parseFlagsFromEnvVars(buildConfiguration.Flags)
	if err != nil {
		return BuildConfiguration{}, err
	}

	if val, ok := os.LookupEnv("BP_GO_BUILD_IMPORT_PATH"); ok {
		buildConfiguration.ImportPath = val
	}

	// BP_DISABLE_MODULE_BOM=false is equivalent to not setting it at all
	// Only if we set BP_DISABLE_MODULE_BOM=true do we skip the generation
	skipBomGeneration := false
	if value, valProvided := os.LookupEnv("BP_DISABLE_MODULE_BOM"); valProvided {
		skipBomGeneration, err = strconv.ParseBool(value)
		if err != nil {
			return BuildConfiguration{}, err
		}
	}

	validGoModFile, err := validateGoModFile(workingDir)
	if err != nil {
		return BuildConfiguration{}, err
	}

	buildConfiguration.GenerateBOM = !skipBomGeneration && validGoModFile

	// if we just check for go.mod file, then we know we are in the go.mod order group and hence the
	// cyclone-gomod buildpack is always required.
	// If we do this in detect then we need a separate way of parsing env vars during build to allow skipping BOM generation
	// If we also parse env vars here, then it might be skipped in the go.mod order group and hence
	// the cyclonedx-gomod buildpack must be listed as optional
	// This allows us to have the same logic in both detect and build

	// check for BP_DISABLE_MODULE_BOM env var and if we are not skipping,
	// then check for presence of go.mod file

	return buildConfiguration, nil
}

func validateGoModFile(workingDir string) (bool, error) {
	_, err := os.Stat(filepath.Join(workingDir, "go.mod"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		} else {
			return false, err
		}
	}

	return true, nil
}

func containsFlag(flags []string, match string) bool {
	for _, flag := range flags {
		if strings.HasPrefix(flag, match) {
			return true
		}
	}
	return false
}

func parseFlagsFromEnvVars(flags []string) ([]string, error) {
	shellwordsParser := shellwords.NewParser()
	shellwordsParser.ParseEnv = true

	if buildFlags, ok := os.LookupEnv("BP_GO_BUILD_FLAGS"); ok {
		var err error
		flags, err = shellwordsParser.Parse(buildFlags)
		if err != nil {
			return nil, err
		}
	}

	if ldFlags, ok := os.LookupEnv("BP_GO_BUILD_LDFLAGS"); ok {
		parsed, err := shellwordsParser.Parse(fmt.Sprintf(`-ldflags="%s"`, ldFlags))
		if err != nil {
			return nil, err
		}
		if len(parsed) != 1 {
			return nil, fmt.Errorf("BP_GO_BUILD_LDFLAGS value (%s) could not be parsed: value contains multiple words", ldFlags)
		}

		for i, flag := range flags {
			if strings.HasPrefix(flag, "-ldflags") {
				// Replace value from BP_GO_BUILD_FLAGS or buildpack.yml with value from
				// BP_GO_BUILD_LDFLAGS because BP_GO_BUILD_LDFLAGS takes precedence
				flags[i] = parsed[0]
			}
		}

		if !containsFlag(flags, "-ldflags") {
			flags = append(flags, parsed[0])
		}
	}
	return flags, nil
}
