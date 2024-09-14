package prometheus

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// getPrometheusCondition executes a Prometheus query and checks if the condition is true.
func GetPrometheusCondition(prometheusURL, prometheusCondition string) (bool, error) {
	// Create a Prometheus API client
	client, err := api.NewClient(api.Config{
		Address: prometheusURL,
	})
	if err != nil {
		return false, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Execute the Prometheus query
	result, warnings, err := v1api.Query(ctx, prometheusCondition, time.Now())
	if err != nil {
		return false, fmt.Errorf("failed to query Prometheus: %w", err)
	}
	if len(warnings) > 0 {
		log.Println("Warnings:", warnings)
	}

	// Evaluate the result. In this case, we are expecting a vector result.
	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		if len(vector) > 0 {
			// Check if the result of the query is 0, which implies the condition is true
			value := vector[0].Value
			return value == 0, nil
		} else {
			// No values returned, meaning the condition isn't met
			return false, nil
		}
	}

	return false, fmt.Errorf("unexpected result type from Prometheus: %v", result.Type())
}
