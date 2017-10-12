/*
Copyright 2017 The Rook Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"path"

	"github.com/rook/rook/pkg/agent/flexvolume"
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:           "rook",
	Short:         "Rook Flex volume plugin",
	SilenceErrors: true,
	SilenceUsage:  true,
}

type NotSupportedError struct{}

func (e *NotSupportedError) Error() string {
	return "Not supported"
}

func Execute() {
	RootCmd.Execute()
}

func getRPCClient() (*rpc.Client, error) {

	ex, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("error getting path of the Rook flexvolume driver: %v", err)
	}
	unixSocketFile := path.Join(path.Dir(ex), path.Join(flexvolume.UnixSocketName)) // /usr/libexec/kubernetes/plugin/volume/rook.io~rook/.rook.sock
	conn, err := net.Dial("unix", unixSocketFile)
	if err != nil {
		return nil, fmt.Errorf("error connecting to socket %s: %+v", unixSocketFile, err)
	}
	return rpc.NewClient(conn), nil
}
