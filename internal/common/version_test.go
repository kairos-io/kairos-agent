package common

import (
	"runtime"
	"testing"
)

func TestGetVersion(t *testing.T) {
	if v := GetVersion(); v == "" {
		t.Fatal("GetVersion returned empty string")
	}
	if v := GetVersion(); v != VERSION {
		t.Fatalf("GetVersion=%q want %q", v, VERSION)
	}
}

func TestGet(t *testing.T) {
	info := Get()
	if info.Version != VERSION {
		t.Fatalf("Version=%q want %q", info.Version, VERSION)
	}
	if info.GitCommit == "" {
		t.Fatal("GitCommit empty")
	}
	if info.GoVersion != runtime.Version() {
		t.Fatalf("GoVersion=%q want %q", info.GoVersion, runtime.Version())
	}
}
