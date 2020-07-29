package v1beta1

import (
	"github.com/devspace-cloud/devspace/pkg/devspace/config/versions/config"
	"github.com/devspace-cloud/devspace/pkg/devspace/config/versions/util"
	next "github.com/devspace-cloud/devspace/pkg/devspace/config/versions/v1beta2"
	"github.com/devspace-cloud/devspace/pkg/util/log"
)

// Upgrade upgrades the config
func (c *Config) Upgrade(log log.Logger) (config.Config, error) {
	nextConfig := &next.Config{}
	err := util.Convert(c, nextConfig)
	if err != nil {
		return nil, err
	}

	// Convert images insecure, dockerfilepath, contextpath, skipPush, build options
	if c.Images != nil {
		for imageConfigName, imageConfig := range *c.Images {
			newImageConfig := (*nextConfig.Images)[imageConfigName]

			if imageConfig.Build != nil && imageConfig.Build.Dockerfile != nil {
				newImageConfig.Dockerfile = imageConfig.Build.Dockerfile
			}
			if imageConfig.Build != nil && imageConfig.Build.Context != nil {
				newImageConfig.Context = imageConfig.Build.Context
			}
			if imageConfig.Insecure != nil {
				if newImageConfig.Build == nil {
					newImageConfig.Build = &next.BuildConfig{}
				}
				if newImageConfig.Build.Kaniko == nil {
					newImageConfig.Build.Kaniko = &next.KanikoConfig{}
				}

				newImageConfig.Build.Kaniko.Insecure = imageConfig.Insecure
			}
			if imageConfig.SkipPush != nil {
				if newImageConfig.Build == nil {
					newImageConfig.Build = &next.BuildConfig{}
				}
				if newImageConfig.Build.Docker == nil {
					newImageConfig.Build.Docker = &next.DockerConfig{}
				}

				newImageConfig.Build.Docker.SkipPush = imageConfig.SkipPush
			}
			if imageConfig.Build != nil && imageConfig.Build.Options != nil {
				if newImageConfig.Build == nil {
					newImageConfig.Build = &next.BuildConfig{}
				}
				if newImageConfig.Build.Kaniko != nil {
					newImageConfig.Build.Kaniko.Options = &next.BuildOptions{
						Target:    imageConfig.Build.Options.Target,
						Network:   imageConfig.Build.Options.Network,
						BuildArgs: imageConfig.Build.Options.BuildArgs,
					}
				} else {
					if newImageConfig.Build.Docker == nil {
						newImageConfig.Build.Docker = &next.DockerConfig{}
					}

					newImageConfig.Build.Docker.Options = &next.BuildOptions{
						Target:    imageConfig.Build.Options.Target,
						Network:   imageConfig.Build.Options.Network,
						BuildArgs: imageConfig.Build.Options.BuildArgs,
					}
				}
			}
		}
	}

	return nextConfig, nil
}

// UpgradeVarPaths upgrades the config
func (c *Config) UpgradeVarPaths(varPaths map[string]string, log log.Logger) error {
	return nil
}
