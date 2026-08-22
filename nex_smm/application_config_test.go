package nex_smm

import "testing"

func TestApplicationConfigEnables100Mario(t *testing.T) {
	config, ok := applicationConfig(0)
	if !ok {
		t.Fatal("application config 0 must exist")
	}
	if len(config) != 40 {
		t.Fatalf("application config 0 has %d entries, want 40", len(config))
	}
	if config[28] != MaxCourseUploads {
		t.Fatalf("upload limit = %d, want %d", config[28], MaxCourseUploads)
	}
	if config[34] != 1 {
		t.Fatalf("Super Expert feature flag = %d, want 1", config[34])
	}
}

func TestApplicationConfigRejectsUnknownID(t *testing.T) {
	if _, ok := applicationConfig(99); ok {
		t.Fatal("unknown application config unexpectedly accepted")
	}
}
