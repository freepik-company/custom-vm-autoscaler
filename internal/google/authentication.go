package google

import (
	"context"
	"custom-vm-autoscaler/api/v1alpha1"

	"google.golang.org/api/option"
)

// createComputeClient creates a Google Cloud Compute client with an optional credentials file.
// The function is generic and works for any type of client (T).
// If GCP CredentialsFile is set, the specified credentials file is used.
// Otherwise, the default credentials are used.
func createComputeClient[T any](ctxConn context.Context, ctx *v1alpha1.Context, clientFunc func(context.Context, ...option.ClientOption) (*T, error)) (*T, error) {

	// Get the path to the credentials file from the environment variable
	if ctx.Config.Infrastructure.GCP.CredentialsFile != "" {
		// If the credentials file is specified, use it
		return clientFunc(ctxConn, option.WithCredentialsFile(ctx.Config.Infrastructure.GCP.CredentialsFile))
	}

	// Use default credentials if no file is specified
	return clientFunc(ctxConn)
}
