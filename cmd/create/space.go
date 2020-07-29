package create

import (
	"github.com/devspace-cloud/devspace-cloud-plugin/cmd/use"
	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud"
	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud/config/versions/latest"
	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/factory"

	"github.com/devspace-cloud/devspace/pkg/devspace/config/loader"
	"github.com/devspace-cloud/devspace/pkg/util/log"
	"github.com/devspace-cloud/devspace/pkg/util/survey"
	"sort"

	"github.com/mgutz/ansi"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// DevSpaceCloudHostedCluster is the option that is shown during cluster select to select the hosted devspace cloud clusters
const DevSpaceCloudHostedCluster = "Clusters managed by DevSpace"

type SpaceCmd struct {
	Active   bool
	Provider string
	Cluster  string
}

func newSpaceCmd(f factory.Factory) *cobra.Command {
	cmd := &SpaceCmd{}

	SpaceCmd := &cobra.Command{
		Use:   "space",
		Short: "Create a new cloud space",
		Long: `
#######################################################
############### devspace create space #################
#######################################################
Creates a new space

Example:
devspace create space myspace
#######################################################
	`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return cmd.RunCreateSpace(f, cobraCmd, args)
		},
	}

	SpaceCmd.Flags().BoolVar(&cmd.Active, "active", true, "Use the new Space as active Space for the current project")
	SpaceCmd.Flags().StringVar(&cmd.Provider, "provider", "", "The cloud provider to use")
	SpaceCmd.Flags().StringVar(&cmd.Cluster, "cluster", "", "The cluster to create a space in")

	return SpaceCmd
}

// RunCreateSpace executes the "devspace create space" command logic
func (cmd *SpaceCmd) RunCreateSpace(f factory.Factory, cobraCmd *cobra.Command, args []string) error {
	// Set config root
	logger := f.GetLog()
	configLoader := loader.NewConfigLoader(nil, logger)
	configExists, err := configLoader.SetDevSpaceRoot()
	if err != nil {
		return err
	}

	// Get provider
	provider, err := f.GetProvider(cmd.Provider, logger)
	if err != nil {
		return err
	}

	logger.StartWait("Retrieving clusters")
	defer logger.StopWait()

	// Get projects
	projects, err := provider.Client().GetProjects()
	if err != nil {
		return errors.Wrap(err, "get projects")
	}

	// Create project if needed
	projectID := 0
	if len(projects) == 0 {
		projectID, err = createProject(provider)
		if err != nil {
			return err
		}
	} else {
		projectID = projects[0].ProjectID
	}

	var cluster *latest.Cluster
	if cmd.Cluster == "" {
		cluster, err = getCluster(provider, logger)
		if err != nil {
			return err
		}
	} else {
		cluster, err = provider.Client().GetClusterByName(cmd.Cluster)
		if err != nil {
			return err
		}
	}

	logger.StartWait("Creating space " + args[0])
	defer logger.StopWait()

	key, err := provider.GetClusterKey(cluster)
	if err != nil {
		return errors.Wrap(err, "get cluster key")
	}

	// Create space
	spaceID, err := provider.Client().CreateSpace(args[0], key, projectID, cluster)
	if err != nil {
		return errors.Wrap(err, "create space")
	}

	// Get Space
	space, err := provider.Client().GetSpace(spaceID)
	if err != nil {
		return errors.Wrap(err, "get space")
	}

	// Get service account
	serviceAccount, err := provider.Client().GetServiceAccount(space, key)
	if err != nil {
		return errors.Wrap(err, "get serviceaccount")
	}

	// Change kube context
	kubeContext := cloud.GetKubeContextNameFromSpace(space.Name, space.ProviderName)
	err = provider.UpdateKubeConfig(kubeContext, serviceAccount, spaceID, true)
	if err != nil {
		return errors.Wrap(err, "update kube context")
	}

	// Cache space
	err = provider.CacheSpace(space, serviceAccount)
	if err != nil {
		return err
	}

	logger.StopWait()
	logger.Infof("Successfully created space %s", space.Name)
	logger.Infof("Your kubectl context has been updated automatically.")

	if configExists {
		logger.Infof("\r         \nYou can now run: \n- `%s` to deploy the app to the cloud\n- `%s` to develop the app in the cloud\n", ansi.Color("devspace deploy", "white+b"), ansi.Color("devspace dev", "white+b"))
	}

	// clear project kube context
	err = use.ClearProjectKubeContext(configLoader)
	if err != nil {
		return errors.Wrap(err, "clear generated kube context")
	}

	return nil
}

func getCluster(p cloud.Provider, logger log.Logger) (*latest.Cluster, error) {
	clusters, err := p.Client().GetClusters()
	if err != nil {
		return nil, errors.Wrap(err, "get clusters")
	}
	if len(clusters) == 0 {
		return nil, errors.New("Cannot create space, because no cluster was found")
	}

	logger.StopWait()

	// Check if the user has access to a connected cluster
	connectedClusters := make([]*latest.Cluster, 0, len(clusters))
	for _, cluster := range clusters {
		if cluster.Owner != nil {
			connectedClusters = append(connectedClusters, cluster)
		}
	}

	// Check if user has connected clusters
	if len(connectedClusters) > 0 {
		clusterNames := []string{}
		for _, cluster := range connectedClusters {
			clusterNames = append(clusterNames, cluster.Name)
		}

		sort.Strings(clusterNames)

		// Check if there are non connected clusters
		for _, cluster := range clusters {
			if cluster.Owner == nil {
				// Add devspace cloud option
				clusterNames = append(clusterNames, DevSpaceCloudHostedCluster)
				break
			}
		}
		if len(clusterNames) == 1 {
			return connectedClusters[0], nil
		}

		// Choose cluster
		chosenCluster, err := logger.Question(&survey.QuestionOptions{
			Question:     "Which cluster should the space created in?",
			DefaultValue: clusterNames[0],
			Options:      clusterNames,
		})
		if err != nil {
			return nil, err
		}

		if chosenCluster != DevSpaceCloudHostedCluster {
			for _, cluster := range connectedClusters {
				if cluster.Name == chosenCluster {
					return cluster, nil
				}
			}
		}
	}

	// Select a devspace cluster
	devSpaceClusters := make([]*latest.Cluster, 0, len(clusters))
	for _, cluster := range clusters {
		if cluster.Owner == nil {
			devSpaceClusters = append(devSpaceClusters, cluster)
		}
	}

	if len(devSpaceClusters) == 1 {
		return devSpaceClusters[0], nil
	}

	clusterNames := []string{}
	for _, cluster := range devSpaceClusters {
		clusterNames = append(clusterNames, cluster.Name)
	}

	// Choose cluster
	chosenCluster, err := logger.Question(&survey.QuestionOptions{
		Question:     "Which hosted DevSpace cluster should the space created in?",
		DefaultValue: clusterNames[0],
		Options:      clusterNames,
	})
	if err != nil {
		return nil, err
	}

	for _, cluster := range devSpaceClusters {
		if cluster.Name == chosenCluster {
			return cluster, nil
		}
	}

	return nil, errors.New("No cluster selected")
}

func createProject(p cloud.Provider) (int, error) {
	return p.Client().CreateProject("default")
}
