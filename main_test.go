package main

import (
	"strings"
	"testing"
)

func TestSortedPlatforms(t *testing.T) {
	checksums := map[string]string{
		"linux/arm64": "bbbb",
		"linux/amd64": "aaaa",
		"linux/s390x": "cccc",
	}

	got := sortedPlatforms(checksums)
	want := []string{"linux/amd64", "linux/arm64", "linux/s390x"}

	if len(got) != len(want) {
		t.Fatalf("sortedPlatforms() returned %d items, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sortedPlatforms()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSortedPlatforms_Empty(t *testing.T) {
	got := sortedPlatforms(nil)
	if len(got) != 0 {
		t.Errorf("sortedPlatforms(nil) returned %d items, want 0", len(got))
	}
}

func TestValidateTools(t *testing.T) {
	tests := []struct {
		name    string
		tools   []Tool
		wantErr string
	}{
		{
			name:    "valid tools",
			tools:   []Tool{{Name: "foo", InstallTool: "curl", Dockerfiles: []string{"all"}}},
			wantErr: "",
		},
		{
			name:    "missing name",
			tools:   []Tool{{InstallTool: "curl", Dockerfiles: []string{"all"}}},
			wantErr: "missing required field 'name'",
		},
		{
			name:    "missing install_tool",
			tools:   []Tool{{Name: "foo", Dockerfiles: []string{"all"}}},
			wantErr: "missing required field 'install_tool'",
		},
		{
			name:    "missing dockerfiles",
			tools:   []Tool{{Name: "foo", InstallTool: "curl"}},
			wantErr: "missing required field 'dockerfiles'",
		},
		{
			name:    "empty list is valid",
			tools:   []Tool{},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTools(tt.tools)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateTools() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("validateTools() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("validateTools() error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestRenderTemplate(t *testing.T) {
	got, err := renderTemplate("test", "Hello {{ .Name }}", funcMap, struct{ Name string }{"World"})
	if err != nil {
		t.Fatalf("renderTemplate() unexpected error: %v", err)
	}
	if got != "Hello World" {
		t.Errorf("renderTemplate() = %q, want %q", got, "Hello World")
	}
}

func TestRenderTemplate_FuncMap(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		data any
		want string
	}{
		{
			name: "title",
			tmpl: "{{ title .S }}",
			data: struct{ S string }{"hello"},
			want: "Hello",
		},
		{
			name: "title empty",
			tmpl: "{{ title .S }}",
			data: struct{ S string }{""},
			want: "",
		},
		{
			name: "replace match",
			tmpl: `{{ replace .S "amd64" "x86_64" }}`,
			data: struct{ S string }{"amd64"},
			want: "x86_64",
		},
		{
			name: "replace no match",
			tmpl: `{{ replace .S "amd64" "x86_64" }}`,
			data: struct{ S string }{"arm64"},
			want: "arm64",
		},
		{
			name: "trimprefix",
			tmpl: `{{ trimprefix .S "v" }}`,
			data: struct{ S string }{"v1.2.3"},
			want: "1.2.3",
		},
		{
			name: "trimprefix no match",
			tmpl: `{{ trimprefix .S "v" }}`,
			data: struct{ S string }{"1.2.3"},
			want: "1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderTemplate("test", tt.tmpl, funcMap, tt.data)
			if err != nil {
				t.Fatalf("renderTemplate() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("renderTemplate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderTemplate_InvalidTemplate(t *testing.T) {
	_, err := renderTemplate("test", "{{ .Invalid", nil, nil)
	if err == nil {
		t.Fatal("renderTemplate() expected error for invalid template, got nil")
	}
}

func TestRenderToolBlock_Curl(t *testing.T) {
	tool := Tool{
		Name:             "tool",
		Source:           "https://github.com/example/tool",
		Version:          "v1.0.0",
		DownloadTemplate: "{{ .Source }}/releases/download/{{ .Version }}/tool_{{ .OS }}_{{ .Arch }}.tar.gz",
		Extract:          "tool",
		InstallTool:      "curl",
		Checksums: map[string]string{
			"linux/amd64": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"linux/arm64": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}

	got, err := renderToolBlock(tool)
	if err != nil {
		t.Fatalf("renderToolBlock() unexpected error: %v", err)
	}

	// Verify it starts with RUN.
	if !strings.HasPrefix(got, "RUN") {
		t.Error("renderToolBlock() output should start with 'RUN'")
	}

	// Verify sorted order: amd64 before arm64.
	amd64Idx := strings.Index(got, "amd64")
	arm64Idx := strings.Index(got, "arm64")
	if amd64Idx == -1 || arm64Idx == -1 {
		t.Fatal("renderToolBlock() output missing platform references")
	}
	if amd64Idx > arm64Idx {
		t.Error("renderToolBlock() amd64 should appear before arm64 (sorted order)")
	}

	// Verify download URL was rendered.
	if !strings.Contains(got, "https://github.com/example/tool/releases/download/v1.0.0/tool_linux_amd64.tar.gz") {
		t.Error("renderToolBlock() output missing expected amd64 download URL")
	}
	if !strings.Contains(got, "https://github.com/example/tool/releases/download/v1.0.0/tool_linux_arm64.tar.gz") {
		t.Error("renderToolBlock() output missing expected arm64 download URL")
	}

	// Verify checksums are present.
	if !strings.Contains(got, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Error("renderToolBlock() output missing amd64 checksum")
	}
	if !strings.Contains(got, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") {
		t.Error("renderToolBlock() output missing arm64 checksum")
	}

	// Verify sha256sum verification is present.
	if !strings.Contains(got, "sha256sum -c") {
		t.Error("renderToolBlock() output missing sha256sum verification")
	}
}

func TestRenderToolBlock_GoInstall(t *testing.T) {
	tool := Tool{
		Name:            "govulncheck",
		Source:          "https://pkg.go.dev/golang.org/x/vuln",
		Version:         "v1.2.0",
		VersionCommit:   "abc123",
		InstallTemplate: "golang.org/x/vuln/cmd/govulncheck@{{ .VersionCommit }}",
		InstallTool:     "go-install",
		Dockerfiles:     []string{"go1.26"},
	}

	got, err := renderToolBlock(tool)
	if err != nil {
		t.Fatalf("renderToolBlock() unexpected error: %v", err)
	}

	want := "RUN go install golang.org/x/vuln/cmd/govulncheck@abc123"
	if got != want {
		t.Errorf("renderToolBlock() = %q, want %q", got, want)
	}
}

func TestRenderToolBlock_Errors(t *testing.T) {
	tests := []struct {
		name    string
		tool    Tool
		wantErr string
	}{
		{
			name: "invalid platform format",
			tool: Tool{
				Name:        "bad",
				InstallTool: "curl",
				Checksums:   map[string]string{"invalid": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			wantErr: "invalid platform format",
		},
		{
			name: "invalid checksum",
			tool: Tool{
				Name:             "bad",
				InstallTool:      "curl",
				DownloadTemplate: "https://example.com/{{ .Arch }}",
				Extract:          "bad",
				Checksums:        map[string]string{"linux/amd64": "not-a-valid-sha256"},
			},
			wantErr: "invalid SHA-256 checksum",
		},
		{
			name: "curl with no checksums",
			tool: Tool{
				Name:        "bad",
				InstallTool: "curl",
				Checksums:   map[string]string{},
			},
			wantErr: "has no checksums defined",
		},
		{
			name: "go-install without install_template",
			tool: Tool{
				Name:        "bad",
				InstallTool: "go-install",
			},
			wantErr: "has no install_template defined",
		},
		{
			name: "unknown install_tool",
			tool: Tool{
				Name:        "bad",
				InstallTool: "unknown",
			},
			wantErr: "unknown install_tool",
		},
		{
			name: "invalid download_template",
			tool: Tool{
				Name:             "bad",
				InstallTool:      "curl",
				DownloadTemplate: "{{ .Invalid }",
				Checksums:        map[string]string{"linux/amd64": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			wantErr: "download_template for bad",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := renderToolBlock(tt.tool)
			if err == nil {
				t.Fatal("renderToolBlock() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("renderToolBlock() error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestTemplateNameForDockerfile(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"go1.25", "Dockerfile.go1.25.tmpl"},
		{"go1.26", "Dockerfile.go1.26.tmpl"},
	}

	for _, tt := range tests {
		got := templateNameForDockerfile(tt.value)
		if got != tt.want {
			t.Errorf("templateNameForDockerfile(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestToolAppliesToTemplate(t *testing.T) {
	tests := []struct {
		name     string
		tool     Tool
		tmplName string
		want     bool
	}{
		{
			name:     "all matches any template",
			tool:     Tool{Dockerfiles: []string{"all"}},
			tmplName: "Dockerfile.go1.25.tmpl",
			want:     true,
		},
		{
			name:     "specific match",
			tool:     Tool{Dockerfiles: []string{"go1.26"}},
			tmplName: "Dockerfile.go1.26.tmpl",
			want:     true,
		},
		{
			name:     "no match",
			tool:     Tool{Dockerfiles: []string{"go1.26"}},
			tmplName: "Dockerfile.go1.25.tmpl",
			want:     false,
		},
		{
			name:     "multiple dockerfiles with match",
			tool:     Tool{Dockerfiles: []string{"go1.25", "go1.26"}},
			tmplName: "Dockerfile.go1.26.tmpl",
			want:     true,
		},
		{
			name:     "empty dockerfiles",
			tool:     Tool{Dockerfiles: []string{}},
			tmplName: "Dockerfile.go1.25.tmpl",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolAppliesToTemplate(tt.tool, tt.tmplName)
			if got != tt.want {
				t.Errorf("toolAppliesToTemplate() = %v, want %v", got, tt.want)
			}
		})
	}
}
