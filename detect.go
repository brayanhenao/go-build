package gobuild

import (
	"github.com/paketo-buildpacks/packit"
)

//go:generate faux --interface ConfigurationParser --output fakes/configuration_parser.go
type ConfigurationParser interface {
	Parse(buildpackVersion, workingDir string) (BuildConfiguration, error)
}

func Detect(parser ConfigurationParser) packit.DetectFunc {
	return func(context packit.DetectContext) (packit.DetectResult, error) {
		buildConfiguration, err := parser.Parse(context.BuildpackInfo.Version, context.WorkingDir)
		if err != nil {
			return packit.DetectResult{}, packit.Fail.WithMessage("failed to parse build configuration: %w", err)
		}

		requirements := []packit.BuildPlanRequirement{
			{
				Name: "go",
				Metadata: map[string]interface{}{
					"build": true,
				},
			},
		}

		if buildConfiguration.GenerateBOM {
			requirements = append(requirements, packit.BuildPlanRequirement{
				Name: "cyclonedx-gomod",
				Metadata: map[string]interface{}{
					"build": true,
				},
			})
		}

		return packit.DetectResult{
			Plan: packit.BuildPlan{
				Requires: requirements,
			},
		}, nil
	}
}
