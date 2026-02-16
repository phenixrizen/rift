package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadNormalizesConfig(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	content := `
sso_start_url: https://example.awsapps.com/start
sso_region: US-EAST-1
regions:
  - us-west-2
  - us-east-1
  - us-west-2
namespace_defaults:
  Prod: kube-system
  DEV: dev-ns
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.SSORegion != "us-east-1" {
		t.Fatalf("SSORegion=%q want us-east-1", cfg.SSORegion)
	}
	if len(cfg.Regions) != 2 || cfg.Regions[0] != "us-east-1" || cfg.Regions[1] != "us-west-2" {
		t.Fatalf("Regions=%v want [us-east-1 us-west-2]", cfg.Regions)
	}
	if got := cfg.NamespaceDefaults["prod"]; got != "kube-system" {
		t.Fatalf("namespace_defaults[prod]=%q want kube-system", got)
	}
	if got := cfg.NamespaceDefaults["dev"]; got != "dev-ns" {
		t.Fatalf("namespace_defaults[dev]=%q want dev-ns", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "rift", "config.yaml")

	cfg := Default()
	cfg.SSOStartURL = "https://example.awsapps.com/start"
	cfg.SSORegion = "us-east-1"
	cfg.NamespaceDefaults = map[string]string{"prod": "kube-system"}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.SSOStartURL != cfg.SSOStartURL || loaded.SSORegion != cfg.SSORegion {
		t.Fatalf("round trip mismatch: got %+v want %+v", loaded, cfg)
	}
}
