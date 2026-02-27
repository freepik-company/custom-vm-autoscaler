package elasticsearch

import (
	"custom-vm-autoscaler/api/v1alpha1"
	"testing"
)

func TestCalculateDesiredReplicas(t *testing.T) {
	tests := []struct {
		name            string
		nodeCount       int
		primaries       int
		currentReplicas int
		maxReplicas     int
		minReplicas     int
		wantDesired     int
		wantUpdate      bool
	}{
		{
			name:            "3 nodes, 1 primary, 1 replica -> increase to 2",
			nodeCount:       3,
			primaries:       1,
			currentReplicas: 1,
			maxReplicas:     5,
			minReplicas:     1,
			wantDesired:     2,
			wantUpdate:      true,
		},
		{
			name:            "5 nodes, 2 primaries, 1 replica -> increase to 2",
			nodeCount:       5,
			primaries:       2,
			currentReplicas: 1,
			maxReplicas:     5,
			minReplicas:     1,
			wantDesired:     2,
			wantUpdate:      true,
		},
		{
			name:            "6 nodes, 3 primaries, 1 replica -> no change",
			nodeCount:       6,
			primaries:       3,
			currentReplicas: 1,
			maxReplicas:     5,
			minReplicas:     1,
			wantDesired:     1,
			wantUpdate:      false,
		},
		{
			name:            "10 nodes, 2 primaries, 2 replicas -> increase to 4",
			nodeCount:       10,
			primaries:       2,
			currentReplicas: 2,
			maxReplicas:     5,
			minReplicas:     1,
			wantDesired:     4,
			wantUpdate:      true,
		},
		{
			name:            "capped by maxReplicas",
			nodeCount:       20,
			primaries:       1,
			currentReplicas: 1,
			maxReplicas:     3,
			minReplicas:     1,
			wantDesired:     3,
			wantUpdate:      true,
		},
		{
			name:            "floor at minReplicas",
			nodeCount:       2,
			primaries:       5,
			currentReplicas: 2,
			maxReplicas:     5,
			minReplicas:     1,
			wantDesired:     1,
			wantUpdate:      true,
		},
		{
			name:            "scale down: 2 nodes, 1 primary, 4 replicas -> reduce to 1",
			nodeCount:       2,
			primaries:       1,
			currentReplicas: 4,
			maxReplicas:     5,
			minReplicas:     1,
			wantDesired:     1,
			wantUpdate:      true,
		},
		{
			name:            "zero primaries returns no update",
			nodeCount:       5,
			primaries:       0,
			currentReplicas: 1,
			maxReplicas:     5,
			minReplicas:     1,
			wantDesired:     1,
			wantUpdate:      false,
		},
		{
			name:            "maxReplicas 0 means no cap",
			nodeCount:       10,
			primaries:       1,
			currentReplicas: 1,
			maxReplicas:     0,
			minReplicas:     1,
			wantDesired:     9,
			wantUpdate:      true,
		},
		{
			name:            "already optimal -> no update",
			nodeCount:       4,
			primaries:       2,
			currentReplicas: 1,
			maxReplicas:     5,
			minReplicas:     1,
			wantDesired:     1,
			wantUpdate:      false,
		},
		{
			name:            "zero nodeCount returns minReplicas",
			nodeCount:       0,
			primaries:       3,
			currentReplicas: 2,
			maxReplicas:     5,
			minReplicas:     1,
			wantDesired:     1,
			wantUpdate:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDesired, gotUpdate := calculateDesiredReplicas(
				tt.nodeCount, tt.primaries, tt.currentReplicas,
				tt.maxReplicas, tt.minReplicas,
			)
			if gotDesired != tt.wantDesired {
				t.Errorf("desired = %d, want %d", gotDesired, tt.wantDesired)
			}
			if gotUpdate != tt.wantUpdate {
				t.Errorf("shouldUpdate = %v, want %v", gotUpdate, tt.wantUpdate)
			}
		})
	}
}

func TestFilterIndices(t *testing.T) {
	indices := []v1alpha1.IndexInfo{
		{Index: "logs-2024-01", Status: "open", Pri: "1", Rep: "1"},
		{Index: "logs-2024-02", Status: "open", Pri: "1", Rep: "1"},
		{Index: "metrics-cpu", Status: "open", Pri: "2", Rep: "1"},
		{Index: ".kibana", Status: "open", Pri: "1", Rep: "1"},
		{Index: ".security", Status: "open", Pri: "1", Rep: "0"},
		{Index: "closed-index", Status: "close", Pri: "1", Rep: "1"},
		{Index: "other-index", Status: "open", Pri: "3", Rep: "1"},
	}

	tests := []struct {
		name          string
		patterns      []string
		includeSystem bool
		wantCount     int
		wantIndices   []string
	}{
		{
			name:          "no patterns, no system -> all open non-system",
			patterns:      nil,
			includeSystem: false,
			wantCount:     4,
			wantIndices:   []string{"logs-2024-01", "logs-2024-02", "metrics-cpu", "other-index"},
		},
		{
			name:          "no patterns, with system -> all open",
			patterns:      nil,
			includeSystem: true,
			wantCount:     6,
			wantIndices:   []string{"logs-2024-01", "logs-2024-02", "metrics-cpu", ".kibana", ".security", "other-index"},
		},
		{
			name:          "logs pattern only",
			patterns:      []string{"logs-*"},
			includeSystem: false,
			wantCount:     2,
			wantIndices:   []string{"logs-2024-01", "logs-2024-02"},
		},
		{
			name:          "multiple patterns",
			patterns:      []string{"logs-*", "metrics-*"},
			includeSystem: false,
			wantCount:     3,
			wantIndices:   []string{"logs-2024-01", "logs-2024-02", "metrics-cpu"},
		},
		{
			name:          "system pattern with includeSystem",
			patterns:      []string{".*"},
			includeSystem: true,
			wantCount:     2,
			wantIndices:   []string{".kibana", ".security"},
		},
		{
			name:          "system pattern without includeSystem -> none",
			patterns:      []string{".*"},
			includeSystem: false,
			wantCount:     0,
			wantIndices:   nil,
		},
		{
			name:          "closed indices always excluded",
			patterns:      []string{"closed-*"},
			includeSystem: false,
			wantCount:     0,
			wantIndices:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterIndices(indices, tt.patterns, tt.includeSystem)
			if len(result) != tt.wantCount {
				t.Errorf("got %d indices, want %d", len(result), tt.wantCount)
				for _, idx := range result {
					t.Logf("  got: %s", idx.Index)
				}
				return
			}
			if tt.wantIndices != nil {
				resultMap := make(map[string]bool)
				for _, idx := range result {
					resultMap[idx.Index] = true
				}
				for _, want := range tt.wantIndices {
					if !resultMap[want] {
						t.Errorf("expected index %s not found in results", want)
					}
				}
			}
		})
	}
}
