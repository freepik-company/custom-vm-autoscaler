package prometheus

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// GetPrometheusCondition executes a Prometheus query and checks if the condition is true.
// prometheusURL: The URL of the Prometheus server.
// prometheusCondition: The Prometheus query condition to be evaluated.
func GetPrometheusCondition(prometheusURL, prometheusCondition string) (bool, error) {
	// Create a Prometheus API client
	client, err := api.NewClient(api.Config{
		Address: prometheusURL, // Set the Prometheus server address
	})
	if err != nil {
		// Return an error if the client fails to be created
		return false, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	// Create a new Prometheus v1 API instance
	v1api := v1.NewAPI(client)

	// Set a timeout context for the query
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel() // Ensure that the context is canceled after query execution

	// Execute the Prometheus query
	result, warnings, err := v1api.Query(ctx, prometheusCondition, time.Now())
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
			value, _ := strconv.ParseInt(vector[0].Value.String(), 10, 32)
			// Return true if the value is 0, which indicates the condition is met
			return value == 1, nil
		} else {
			// No values returned, so the condition is not met
			return false, nil
		}
	}

	// Return an error if the result type is unexpected
	return false, fmt.Errorf("unexpected result type from Prometheus: %v", result.Type())
}
