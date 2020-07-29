package testing

import (
	"fmt"

	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud"
	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud/client"
	testClient "github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud/client/testing"
	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud/config/versions/latest"
	"github.com/pkg/errors"
)

type provider struct {
	config latest.Provider
	client client.Client
}

// NewFakeProvider creates a new instance of the provider interface
func NewFakeProvider(config latest.Provider, client client.Client) cloud.Provider {
	if client == nil {
		client = &testClient.CloudClient{}
	}
	return &provider{
		config: config,
		client: client,
	}
}

func (p *provider) GetAndUpdateSpaceCache(spaceID int, forceUpdate bool) (*latest.SpaceCache, bool, error) {
	space, ok := p.config.Spaces[spaceID]
	if ok && !forceUpdate {
		return space, false, nil
	}

	p.CacheSpace(&latest.Space{Name: fmt.Sprintf("space-%d", spaceID), SpaceID: spaceID}, &latest.ServiceAccount{SpaceID: spaceID})
	return p.config.Spaces[spaceID], true, nil
}

func (p *provider) CacheSpace(space *latest.Space, serviceAccount *latest.ServiceAccount) error {
	cachedSpace := &latest.SpaceCache{
		Space:          space,
		ServiceAccount: serviceAccount,

		KubeContext: space.ProviderName + "-" + space.Name,
	}

	if p.config.Spaces == nil {
		p.config.Spaces = map[int]*latest.SpaceCache{}
	}

	p.config.Spaces[space.SpaceID] = cachedSpace

	return nil
}

func (p *provider) ConnectCluster(options *cloud.ConnectClusterOptions) error {
	return nil
}
func (p *provider) ResetKey(clusterName string) error {
	var cluster *latest.Cluster
	for _, space := range p.config.Spaces {
		if space.Space.Cluster.Name == clusterName {
			cluster = space.Space.Cluster
			break
		}
	}

	if cluster == nil {
		return errors.Errorf("Cluster %s not found", clusterName)
	}
	p.config.ClusterKey[cluster.ClusterID] = "reset"
	return nil
}

func (p *provider) UpdateKubeConfig(contextName string, serviceAccount *latest.ServiceAccount, spaceID int, setActive bool) error {
	return nil
}
func (p *provider) DeleteKubeContext(space *latest.Space) error {
	return nil
}

func (p *provider) GetClusterKey(cluster *latest.Cluster) (string, error) {
	key, ok := p.config.ClusterKey[cluster.ClusterID]
	if ok {
		return key, nil
	}
	return "", errors.Errorf("No cluster key for %d", cluster.ClusterID)
}

func (p *provider) PrintToken(spaceID int) error {
	return nil
}
func (p *provider) PrintSpaces(cluster, name string, all bool) error {
	return nil
}

func (p *provider) Save() error {
	return nil
}
func (p *provider) Client() client.Client {
	return p.client
}
func (p *provider) GetConfig() latest.Provider {
	return p.config
}
