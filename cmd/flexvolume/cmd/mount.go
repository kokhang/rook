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
	"encoding/json"
	"fmt"

	"github.com/rook/rook/pkg/agent/flexvolume"
	"github.com/spf13/cobra"
)

var (
	mountCmd = &cobra.Command{
		Use:   "mount",
		Short: "Mounts the volume to the pod volume",
		RunE:  mount,
	}
)

func init() {
	RootCmd.AddCommand(mountCmd)
}

func mount(cmd *cobra.Command, args []string) error {

	client, err := getRPCClient()
	if err != nil {
		return fmt.Errorf("Rook: Error getting RPC client: %v", err)
	}

	var opts = &flexvolume.AttachOptions{}
	if err := json.Unmarshal([]byte(args[1]), opts); err != nil {
		return fmt.Errorf("Rook: Could not parse options for mounting %s. Got %v", args[1], err)
	}
	opts.MountDir = args[0]

	// Call agent to perform mount operation
	err = client.Call("FlexvolumeController.Mount", opts, nil)
	if err != nil {
		return fmt.Errorf("Rook: Mount volume failed: %v", err)
	}
	return nil
}
