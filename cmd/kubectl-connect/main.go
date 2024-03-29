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

package main

import (
	"fmt"
	"os"

	apiservicecmd "go.bytebuilders.dev/kube-bind/pkg/kubectl/bind-apiservice/cmd"
	bindcmd "go.bytebuilders.dev/kube-bind/pkg/kubectl/bind/cmd"

	"github.com/spf13/pflag"
	v "gomodules.xyz/x/version"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func main() {
	flags := pflag.NewFlagSet("kubectl-connect", pflag.ExitOnError)
	pflag.CommandLine = flags

	bindCmd, err := bindcmd.New(genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v", err)
		os.Exit(1)
	}

	apiserviceCmd, err := apiservicecmd.New(genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v", err)
		os.Exit(1)
	}
	bindCmd.AddCommand(apiserviceCmd)
	bindCmd.AddCommand(v.NewCmdVersion())

	if err := bindCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
