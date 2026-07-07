package handlers

import "testing"

func TestEffectiveCapacity(t *testing.T) {
	cases := []struct {
		name                string
		totalMB             int64
		overallocatePercent int
		want                int64
	}{
		{"no overallocation", 4096, 0, 4096},
		{"20 percent overallocation", 4096, 20, 4915},
		{"100 percent overallocation doubles capacity", 2048, 100, 4096},
		{"zero total stays zero", 0, 50, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := effectiveCapacity(tc.totalMB, tc.overallocatePercent)
			if got != tc.want {
				t.Fatalf("effectiveCapacity(%d, %d) = %d, want %d", tc.totalMB, tc.overallocatePercent, got, tc.want)
			}
		})
	}
}

func TestEffectiveCapacityRejectsOverCommit(t *testing.T) {
	capacity := effectiveCapacity(4096, 20)
	used := int64(4000)

	if used+1000 <= capacity {
		t.Fatalf("expected 4000+1000 to exceed capacity %d, but it didn't", capacity)
	}
	if used+900 > capacity {
		t.Fatalf("expected 4000+900 to fit within capacity %d, but it didn't", capacity)
	}
}
