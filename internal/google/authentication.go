package google

import (
	"context"
	"os"

	"google.golang.org/api/option"
)

func createComputeClient[T any](ctx context.Context, clientFunc func(context.Context, ...option.ClientOption) (*T, error)) (*T, error) {
	credentialsFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if credentialsFile != "" {
		// Usar credenciales del archivo especificado en GOOGLE_APPLICATION_CREDENTIALS
		return clientFunc(ctx, option.WithCredentialsFile(credentialsFile))
	}

	// Usar credenciales predeterminadas
	return clientFunc(ctx)
}
