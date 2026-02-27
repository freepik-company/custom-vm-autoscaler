package elasticsearch

import (
	"custom-vm-autoscaler/api/v1alpha1"
	"testing"
)

func TestCalculateDesiredReplicas(t *testing.T) {
	tests := []struct {
		name           string
		nodeCount      int
		totalPrimaries int
		maxReplicas    int
		minReplicas    int
		wantDesired    int
	}{
		{
			name:           "images-v9: 17 nodes, 45 total primaries (9 idx × 5 pri) -> 1 replica (no change needed)",
			nodeCount:      17,
			totalPrimaries: 45,
			maxReplicas:    0,
			minReplicas:    1,
			wantDesired:    1,
		},
		{
			name:           "images-v9: 100 nodes, 45 total primaries -> 2 replicas",
			nodeCount:      100,
			totalPrimaries: 45,
			maxReplicas:    0,
			minReplicas:    1,
			wantDesired:    2,
		},
		{
			name:           "pikaso: 30 nodes, 320 total primaries (64 idx × 5 pri) -> 1 replica (no change)",
			nodeCount:      30,
			totalPrimaries: 320,
			maxReplicas:    0,
			minReplicas:    1,
			wantDesired:    1,
		},
		{
			name:           "pikaso: 700 nodes, 320 total primaries -> 2 replicas",
			nodeCount:      700,
			totalPrimaries: 320,
			maxReplicas:    0,
			minReplicas:    1,
			wantDesired:    2,
		},
		{
			name:           "few shards many nodes: 50 nodes, 10 total primaries -> 4 replicas",
			nodeCount:      50,
			totalPrimaries: 10,
			maxReplicas:    0,
			minReplicas:    1,
			wantDesired:    4,
		},
		{
			name:           "capped by maxReplicas",
			nodeCount:      100,
			totalPrimaries: 10,
			maxReplicas:    3,
			minReplicas:    1,
			wantDesired:    3,
		},
		{
			name:           "floor at minReplicas when shards >> nodes",
			nodeCount:      2,
			totalPrimaries: 500,
			maxReplicas:    5,
			minReplicas:    1,
			wantDesired:    1,
		},
		{
			name:           "zero totalPrimaries returns minReplicas",
			nodeCount:      5,
			totalPrimaries: 0,
			maxReplicas:    5,
			minReplicas:    1,
			wantDesired:    1,
		},
		{
			name:           "zero nodeCount returns minReplicas",
			nodeCount:      0,
			totalPrimaries: 45,
			maxReplicas:    5,
			minReplicas:    1,
			wantDesired:    1,
		},
		{
			name:           "exact fit: 10 nodes, 5 total primaries -> 1 replica",
			nodeCount:      10,
			totalPrimaries: 5,
			maxReplicas:    0,
			minReplicas:    1,
			wantDesired:    1,
		},
		{
			name:           "just over: 11 nodes, 5 total primaries -> 2 replicas",
			nodeCount:      11,
			totalPrimaries: 5,
			maxReplicas:    0,
			minReplicas:    1,
			wantDesired:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateDesiredReplicas(
				tt.nodeCount, tt.totalPrimaries,
				tt.maxReplicas, tt.minReplicas,
			)
			if got != tt.wantDesired {
				t.Errorf("desired = %d, want %d", got, tt.wantDesired)
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
