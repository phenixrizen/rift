package naming

import "testing"

func TestSlug(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "basic", input: "Customer Prod", want: "customer-prod"},
		{name: "collapse separators", input: "  A__B---C  ", want: "a-b-c"},
		{name: "invalid chars", input: "Team/Platform@EKS", want: "team-platform-eks"},
		{name: "empty", input: "   ", want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slug(tt.input)
			if got != tt.want {
				t.Fatalf("Slug(%q)=%q want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInferEnv(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{name: "prod", parts: []string{"acme-production", "Admin"}, want: "prod"},
		{name: "staging", parts: []string{"acme-staging", "Developer"}, want: "staging"},
		{name: "dev", parts: []string{"acme-dev", "Developer"}, want: "dev"},
		{name: "int", parts: []string{"acme", "integration"}, want: "int"},
		{name: "other", parts: []string{"sandbox", "ops"}, want: "other"},
		{name: "contains int", parts: []string{"print-service"}, want: "int"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferEnv(tt.parts...)
			if got != tt.want {
				t.Fatalf("InferEnv(%q)=%q want %q", tt.parts, got, tt.want)
			}
		})
	}
}
