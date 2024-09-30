package prometheus

import (
	"context"
	"custom-vm-autoscaler/api/v1alpha1"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// customTransport is an HTTP transport that adds custom headers to requests.
type customTransport struct {
	Transport http.RoundTripper
	Config    *v1alpha1.ConfigSpec
}

// RoundTrip executes a single HTTP transaction and adds custom headers.
func (t *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Set headers from the config file
	for headerName, headerValue := range t.Config.Metrics.Prometheus.Headers {
		req.Header.Set(headerName, headerValue)
	}

	return t.Transport.RoundTrip(req)
}

// GetPrometheusCondition executes a Prometheus query and checks if the condition is true.
// prometheusURL: The URL of the Prometheus server.
// prometheusCondition: The Prometheus query condition to be evaluated.
func GetPrometheusCondition(prometheusCondition string, ctx *v1alpha1.Context) (bool, error) {

	// Create a custom HTTP client with the custom transport
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &customTransport{
			Transport: http.DefaultTransport,
			Config:    ctx.Config},
	}

	// Create a Prometheus API client
	client, err := api.NewClient(api.Config{
		Address: ctx.Config.Metrics.Prometheus.URL, // Set the Prometheus server address
		Client:  httpClient,
	})
	if err != nil {
		// Return an error if the client fails to be created
		return false, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	// Create a new Prometheus v1 API instance
	v1api := v1.NewAPI(client)

	// Set a timeout context for the query
	ctxConn, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel() // Ensure that the context is canceled after query execution

	// Execute the Prometheus query
	result, warnings, err := v1api.Query(ctxConn, prometheusCondition, time.Now())
	if err != nil {
		// Return an error if the query fails
		return false, fmt.Errorf("failed to query Prometheus: %w", err)
	}
	if len(warnings) > 0 {
		// Log any warnings returned by the Prometheus query
		log.Println("Warnings:", warnings)
	}
	// Check if the result is a vector (expected format)
	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		// If the vector has results, check the first value
		if len(vector) > 0 {
			// Return true if vector has any value, which indicates the condition is met
			return true, nil
		} else {
			// No values returned, so the condition is not met
			return false, nil
		}
	}

	// Return an error if the result type is unexpected
	return false, fmt.Errorf("unexpected result type from Prometheus: %v", result.Type())
}
