package remove

import (
	"fmt"

	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/factory"
	"github.com/devspace-cloud/devspace/pkg/util/survey"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type clusterCmd struct {
	Provider string
	AllYes   bool
}

func newClusterCmd(f factory.Factory) *cobra.Command {
	cmd := &clusterCmd{}

	clusterCmd := &cobra.Command{
		Use:   "cluster",
		Short: "Removes a connected cluster",
		Long: `
#######################################################
############# devspace remove cluster #################
#######################################################
Removes a connected cluster 

Example:
devspace remove cluster my-cluster
#######################################################
	`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return cmd.RunRemoveCluster(f, cobraCmd, args)
		}}

	clusterCmd.Flags().StringVar(&cmd.Provider, "provider", "", "The cloud provider to use")
	clusterCmd.Flags().BoolVarP(&cmd.AllYes, "yes", "y", false, "Ignores all questions and deletes the cluster with all services and spaces")

	return clusterCmd
}

// RunRemoveCluster executes the devspace remove cluster functionality
func (cmd *clusterCmd) RunRemoveCluster(f factory.Factory, cobraCmd *cobra.Command, args []string) error {
	// Get provider
	log := f.GetLog()
	provider, err := f.GetProvider(cmd.Provider, log)
	if err != nil {
		return errors.Wrap(err, "log into provider")
	}

	if cmd.AllYes == false {
		// Verify user is sure to delete the cluster
		deleteCluster, err := log.Question(&survey.QuestionOptions{
			Question:     fmt.Sprintf("Are you sure you want to delete cluster %s? This action is irreversible", args[0]),
			DefaultValue: "No",
			Options: []string{
				"No",
				"Yes",
			},
		})
		if err != nil {
			return err
		}
		if deleteCluster != "Yes" {
			return nil
		}
	}

	// Get cluster by name
	cluster, err := provider.Client().GetClusterByName(args[0])
	if err != nil {
		return err
	}

	// Delete all spaces?
	var (
		deleteSpaces   = "Yes"
		deleteServices = "Yes"
	)

	if cmd.AllYes == false {
		deleteSpaces, err = log.Question(&survey.QuestionOptions{
			Question:     "Do you want to delete all cluster spaces?",
			DefaultValue: "No",
			Options: []string{
				"No",
				"Yes",
			},
		})
		if err != nil {
			return err
		}

		// Delete services
		deleteServices, err = log.Question(&survey.QuestionOptions{
			Question:     "Do you want to delete all cluster services?",
			DefaultValue: "No",
			Options: []string{
				"No",
				"Yes",
			},
		})
		if err != nil {
			return err
		}
	}

	// Delete cluster
	log.StartWait("Deleting cluster " + cluster.Name)

	key, err := provider.GetClusterKey(cluster)
	if err != nil {
		return errors.Wrap(err, "get cluster key")
	}

	err = provider.Client().DeleteCluster(cluster, key, deleteServices == "Yes", deleteSpaces == "Yes")
	if err != nil {
		return err
	}
	log.StopWait()

	providerConfig := provider.GetConfig()
	delete(providerConfig.ClusterKey, cluster.ClusterID)
	err = provider.Save()
	if err != nil {
		return err
	}

	log.Donef("Successfully deleted cluster %s", args[0])
	return nil
}
