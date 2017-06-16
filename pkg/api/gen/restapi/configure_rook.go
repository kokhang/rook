package restapi

import (
	"crypto/tls"
	"net/http"

	errors "github.com/go-openapi/errors"
	runtime "github.com/go-openapi/runtime"
	middleware "github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/runtime/yamlpc"

	"github.com/rook/rook/pkg/api/gen/restapi/operations"
)

// This file is safe to edit. Once it exists it will not be overwritten

//go:generate swagger generate server --target ../pkg/api/gen --name rook --spec ../pkg/api/swagger.yml --exclude-main

func configureFlags(api *operations.RookAPI) {
	// api.CommandLineOptionsGroups = []swag.CommandLineOptionsGroup{ ... }
}

func configureAPI(api *operations.RookAPI) http.Handler {
	// configure the api here
	api.ServeError = errors.ServeError

	// Set your custom logger if needed. Default one is log.Printf
	// Expected interface func(string, ...interface{})
	//
	// Example:
	// s.api.Logger = log.Printf

	api.YamlConsumer = yamlpc.YAMLConsumer()

	api.JSONConsumer = runtime.JSONConsumer()

	api.YamlProducer = yamlpc.YAMLProducer()

	api.JSONProducer = runtime.JSONProducer()

	api.CreateVolumeAttachmentHandler = operations.CreateVolumeAttachmentHandlerFunc(func(params operations.CreateVolumeAttachmentParams) middleware.Responder {
		return middleware.NotImplemented("operation .CreateVolumeAttachment has not yet been implemented")
	})
	api.DeleteVolumeAttachmentHandler = operations.DeleteVolumeAttachmentHandlerFunc(func(params operations.DeleteVolumeAttachmentParams) middleware.Responder {
		return middleware.NotImplemented("operation .DeleteVolumeAttachment has not yet been implemented")
	})
	api.GetVolumeAttachmentHandler = operations.GetVolumeAttachmentHandlerFunc(func(params operations.GetVolumeAttachmentParams) middleware.Responder {
		return middleware.NotImplemented("operation .GetVolumeAttachment has not yet been implemented")
	})
	api.ListVolumeAttachmentHandler = operations.ListVolumeAttachmentHandlerFunc(func(params operations.ListVolumeAttachmentParams) middleware.Responder {
		return middleware.NotImplemented("operation .ListVolumeAttachment has not yet been implemented")
	})

	api.ServerShutdown = func() {}

	return setupGlobalMiddleware(api.Serve(setupMiddlewares))
}

// The TLS configuration before HTTPS server starts.
func configureTLS(tlsConfig *tls.Config) {
	// Make all necessary changes to the TLS configuration here.
}

// The middleware configuration is for the handler executors. These do not apply to the swagger.json document.
// The middleware executes after routing but before authentication, binding and validation
func setupMiddlewares(handler http.Handler) http.Handler {
	return handler
}

// The middleware configuration happens before anything, this middleware also applies to serving the swagger.json document.
// So this is a good place to plug in a panic handling middleware, logging and metrics
func setupGlobalMiddleware(handler http.Handler) http.Handler {
	return handler
}
