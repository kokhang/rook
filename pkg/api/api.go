/*
Copyright 2016 The Rook Authors. All rights reserved.

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
package api

import (
	"flag"
	"fmt"

	"github.com/go-openapi/loads"
	"github.com/rook/rook/pkg/api/gen/restapi"
	"github.com/rook/rook/pkg/api/gen/restapi/operations"
)

// Start the Rook API
func Start(errChan chan error) {
	logger.Infof("starting the Rook Operator API")

	var portFlag = flag.Int("port", 8124, "Port to run this service on")
	// load embedded swagger file
	swaggerSpec, err := loads.Analyzed(restapi.SwaggerJSON, "")
	if err != nil {
		errChan <- fmt.Errorf("failed to load API spec. %+v", err)
		return
	}

	// create new service API
	api := operations.NewRookAPI(swaggerSpec)
	server := restapi.NewServer(api)
	defer server.Shutdown()

	// parse flags
	flag.Parse()
	// set the port this service will be run on
	server.Port = *portFlag

	server.ConfigureAPI()

	// serve API
	if err := server.Serve(); err != nil {
		errChan <- fmt.Errorf("failed to start API. %+v", err)
	}
}
