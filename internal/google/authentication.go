package google

import (
	"context"
	"os"

	"google.golang.org/api/option"
)

// createComputeClient creates a Google Cloud Compute client with an optional credentials file.
// The function is generic and works for any type of client (T).
// If GOOGLE_APPLICATION_CREDENTIALS is set, the specified credentials file is used.
// Otherwise, the default credentials are used.
func createComputeClient[T any](ctx context.Context, clientFunc func(context.Context, ...option.ClientOption) (*T, error)) (*T, error) {

	// Get the path to the credentials file from the environment variable
	credentialsFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if credentialsFile != "" {
		// If the credentials file is specified, use it
		return clientFunc(ctx, option.WithCredentialsFile(credentialsFile))
	}

	// Use default credentials if no file is specified
	return clientFunc(ctx)
}
