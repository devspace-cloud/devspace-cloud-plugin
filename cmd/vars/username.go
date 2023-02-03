package vars

import (
	"fmt"
	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud"
	cloudconfig "github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud/config"
	cloudtoken "github.com/devspace-cloud/devspace-cloud-plugin/pkg/cloud/token"
	"github.com/devspace-cloud/devspace-cloud-plugin/pkg/factory"
	"github.com/devspace-cloud/devspace/pkg/util/log"
	"github.com/mgutz/ansi"
	"os"

	"github.com/spf13/cobra"
)

type usernameCmd struct{}

func newUsernameCmd(f factory.Factory) *cobra.Command {
	cmd := &usernameCmd{}

	return &cobra.Command{
		Use:   "username",
		Short: "Prints the current username",
		Args:  cobra.NoArgs,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return cmd.Run(f, cobraCmd, args)
		},
	}
}

// Run executes the command logic
func (*usernameCmd) Run(f factory.Factory, cobraCmd *cobra.Command, args []string) error {
	retErr := fmt.Errorf("You are not logged into DevSpace Cloud, but predefined var DEVSPACE_USERNAME is used.\n\nPlease run: \n- `%s` to login into devspace cloud. Alternatively you can also remove the variable ${DEVSPACE_USERNAME} from your config", ansi.Color("devspace login", "white+b"))

	kubeLoader := f.NewKubeConfigLoader()
	kubeContext, err := kubeLoader.GetCurrentContext()
	if err != nil {
		return err
	}
	if os.Getenv("DEVSPACE_PLUGIN_KUBE_CONTEXT_FLAG") != "" {
		kubeContext = os.Getenv("DEVSPACE_PLUGIN_KUBE_CONTEXT_FLAG")
	}

	loader := cloudconfig.NewLoader()
	cloudConfigData, err := loader.Load()
	if err != nil {
		return err
	}

	_, providerName, err := kubeLoader.GetSpaceID(kubeContext)
	if err != nil {
		// use global provider config as fallback
		if cloudConfigData.Default != "" {
			providerName = cloudConfigData.Default
		} else {
			providerName = cloudconfig.DevSpaceCloudProviderName
		}
	}

	provider := cloudconfig.GetProvider(cloudConfigData, providerName)
	if provider == nil {
		return retErr
	}

	token := provider.Token
	if provider.Token == "" {
		prov, err := cloud.GetProvider("", log.Discard)
		if err != nil {
			return retErr
		}
		
		token, err = prov.Client().GetToken()
		if err != nil {
			return retErr
		}
	}

	accountName, err := cloudtoken.GetAccountName(token)
	if err != nil {
		fmt.Println(err)
		return retErr
	}

	_, err = os.Stdout.Write([]byte(accountName))
	return err
}
