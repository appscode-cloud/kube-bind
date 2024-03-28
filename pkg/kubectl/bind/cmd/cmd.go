/*
Copyright AppsCode Inc. and Contributors

Licensed under the AppsCode Community License 1.0.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://github.com/appscode/licenses/raw/1.0.0/AppsCode-Community-1.0.0.md

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"fmt"
	"strings"

	"go.bytebuilders.dev/kube-bind/pkg/kubectl/bind/plugin"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth/exec"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	logsv1 "k8s.io/component-base/logs/api/v1"
)

// TODO: add other examples related to permission claim commands.
var bindExampleUses = `
	# select a kube-bind.appscode.com compatible service from the given URL, e.g. an API service.
	%[1]s bind https://mangodb.com/exports

	# authenticate and configure the services to bind, but don't actually bind them.
	%[1]s bind https://mangodb.com/exports --dry-run -o yaml > apiservice-export-requests.yaml

	# bind to a remote API service as configured above and actually bind to it, e.g. in GitOps automation.
	%[1]s bind apiservice --remote-kubeconfig name -f apiservice-binding-requests.yaml

	# bind to a remote API service via a request manifest from a https URL.
	%[1]s bind apiservice --remote-kubeconfig name https://some-url.com/apiservice-export-requests.yaml
	`

func New(streams genericclioptions.IOStreams) (*cobra.Command, error) {
	opts := plugin.NewBindOptions(streams)
	cmd := &cobra.Command{
		Use:          "connect",
		Short:        "Bind different remote types into the current cluster.",
		Example:      fmt.Sprintf(bindExampleUses, "kubectl"),
		SilenceUsage: true,
		Args: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if !strings.HasPrefix(arg, "http://") && !strings.HasPrefix(arg, "https://") {
					return fmt.Errorf("unknown argument: %s", arg) // this will fall back to sub-commands
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := logsv1.ValidateAndApply(opts.Logs, nil); err != nil {
				return err
			}

			if len(args) == 0 {
				return cmd.Help()
			}
			if err := opts.Complete(args); err != nil {
				return err
			}

			if err := opts.Validate(); err != nil {
				return err
			}

			return opts.Run(cmd.Context(), nil)
		},
	}
	opts.AddCmdFlags(cmd)

	return cmd, nil
}
