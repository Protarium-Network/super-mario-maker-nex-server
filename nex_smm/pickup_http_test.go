package nex_smm

import "testing"

func TestPickupPoolSize(t *testing.T) {
	tests := []struct {
		path string
		want int
		ok   bool
	}{
		{"/api/v1/pickup/easy", 8, true},
		{"/api/v1/pickup/normal", 16, true},
		{"/api/v1/pickup/expert", 16, true},
		{"/api/v1/pickup/super_expert", 6, true},
		{"/api/v1/pickup/unknown", 0, false},
	}

	for _, test := range tests {
		got, ok := pickupPoolSize(test.path)
		if got != test.want || ok != test.ok {
			t.Fatalf("pickupPoolSize(%q) = (%d, %v), want (%d, %v)", test.path, got, ok, test.want, test.ok)
		}
	}
}
