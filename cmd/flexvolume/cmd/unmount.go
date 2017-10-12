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

	"github.com/rook/rook/pkg/agent/flexvolume"
	"github.com/spf13/cobra"
)

var (
	unmountCmd = &cobra.Command{
		Use:   "unmount",
		Short: "Unmounts the pod volume",
		RunE:  unmount,
	}
)

func init() {
	RootCmd.AddCommand(unmountCmd)
}

func unmount(cmd *cobra.Command, args []string) error {

	client, err := getRPCClient()
	if err != nil {
		return fmt.Errorf("Rook: Error getting RPC client: %v", err)
	}

	var opts = &flexvolume.AttachOptions{
		MountDir: args[0],
	}

	// Call agent to perform unmount operation
	err = client.Call("FlexvolumeController.Unmount", opts, nil)
	if err != nil {
		return fmt.Errorf("Rook: Unmount volume failed: %v", err)
	}
	return nil
}
