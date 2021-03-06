package factory

import (
	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud"
	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud/config"
	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud/resume"
	"github.com/devspace-cloud/devspace/pkg/devspace/config/loader"
	"github.com/devspace-cloud/devspace/pkg/devspace/kubectl"
	"github.com/devspace-cloud/devspace/pkg/util/kubeconfig"
	"github.com/devspace-cloud/devspace/pkg/util/log"
)

// Factory is the main interface for various client creations
type Factory interface {
	// Config Loader
	NewConfigLoader(options *loader.ConfigOptions, log log.Logger) loader.ConfigLoader

	// Kubernetes Clients
	NewKubeClientFromContext(context, namespace string, switchContext bool) (kubectl.Client, error)

	// Cloud
	GetProvider(useProviderName string, log log.Logger) (cloud.Provider, error)
	GetProviderWithOptions(useProviderName, key string, relogin bool, loader config.Loader, kubeLoader kubeconfig.Loader, log log.Logger) (cloud.Provider, error)
	NewSpaceResumer(kubeClient kubectl.Client, log log.Logger) resume.SpaceResumer
	NewCloudConfigLoader() config.Loader

	// Kubeconfig
	NewKubeConfigLoader() kubeconfig.Loader

	// Log
	GetLog() log.Logger
}

// DefaultFactoryImpl is the default factory implementation
type DefaultFactoryImpl struct{}

// DefaultFactory returns the default factory implementation
func DefaultFactory() Factory {
	return &DefaultFactoryImpl{}
}

// NewCloudConfigLoader creates a new cloud config loader
func (f *DefaultFactoryImpl) NewCloudConfigLoader() config.Loader {
	return config.NewLoader()
}

// NewKubeConfigLoader implements interface
func (f *DefaultFactoryImpl) NewKubeConfigLoader() kubeconfig.Loader {
	return kubeconfig.NewLoader()
}

// GetLog implements interface
func (f *DefaultFactoryImpl) GetLog() log.Logger {
	return log.GetInstance()
}

// NewConfigLoader implements interface
func (f *DefaultFactoryImpl) NewConfigLoader(options *loader.ConfigOptions, log log.Logger) loader.ConfigLoader {
	return loader.NewConfigLoader(options, log)
}

// NewKubeClientFromContext implements interface
func (f *DefaultFactoryImpl) NewKubeClientFromContext(context, namespace string, switchContext bool) (kubectl.Client, error) {
	kubeLoader := f.NewKubeConfigLoader()
	return kubectl.NewClientFromContext(context, namespace, switchContext, kubeLoader)
}

// GetProvider implements interface
func (f *DefaultFactoryImpl) GetProvider(useProviderName string, log log.Logger) (cloud.Provider, error) {
	return cloud.GetProvider(useProviderName, log)
}

// GetProviderWithOptions implements interface
func (f *DefaultFactoryImpl) GetProviderWithOptions(useProviderName, key string, relogin bool, loader config.Loader, kubeLoader kubeconfig.Loader, log log.Logger) (cloud.Provider, error) {
	return cloud.GetProviderWithOptions(useProviderName, key, relogin, loader, kubeLoader, log)
}

// NewSpaceResumer implements interface
func (f *DefaultFactoryImpl) NewSpaceResumer(kubeClient kubectl.Client, log log.Logger) resume.SpaceResumer {
	return resume.NewSpaceResumer(kubeClient, log)
}
