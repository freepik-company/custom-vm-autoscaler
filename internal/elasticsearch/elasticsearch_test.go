package elasticsearch

import (
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
			name:           "image-v9: 17 nodes, 45 total primaries (9 idx × 5 pri) -> 1 replica (no change needed)",
			nodeCount:      17,
			totalPrimaries: 45,
			maxReplicas:    0,
			minReplicas:    1,
			wantDesired:    1,
		},
		{
			name:           "image-v9: 100 nodes, 45 total primaries -> 2 replicas",
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
