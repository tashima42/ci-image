package main

import (
	"bytes"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"go.yaml.in/yaml/v4"
)

const (
	templatesDir       = "templates"
	dockerfilesDir     = "dockerfiles"
	dependenciesMarker = "### dependencies-template"
	templateSuffix     = ".tmpl"
)

// Deps is the top-level structure of deps.yaml.
type Deps struct {
	Tools []Tool `yaml:"tools"`
}

// Tool represents a single tool entry in deps.yaml.
type Tool struct {
	Name             string            `yaml:"name"`
	Source           string            `yaml:"source"`
	Version          string            `yaml:"version"`
	VersionCommit    string            `yaml:"version_commit,omitempty"`
	DownloadTemplate string            `yaml:"download_template,omitempty"`
	InstallTemplate  string            `yaml:"install_template,omitempty"`
	Extract          string            `yaml:"extract,omitempty"`
	InstallTool      string            `yaml:"install_tool"`
	Checksums        map[string]string `yaml:"checksums,omitempty"`
	Dockerfiles      []string          `yaml:"dockerfiles"`
}

// DockerfileToolVars is the data passed into installCmds templates.
type DockerfileToolVars struct {
	Name           string
	InstallPackage string
	Platforms      []PlatformResolved
}

// PlatformResolved holds the resolved URLs and checksums for one os/arch.
type PlatformResolved struct {
	Arch        string
	DownloadURL string
	Extract     string
	Checksum    string
}

// TemplateVars is the data passed into each template execution.
type TemplateVars struct {
	Source        string
	Version       string
	VersionCommit string
	OS            string
	Arch          string
}

// Custom functions available inside the templates.
var funcMap = template.FuncMap{
	"title": func(s string) string {
		if len(s) == 0 {
			return s
		}
		return strings.ToUpper(s[:1]) + s[1:]
	},
	"replace": func(s, old, new string) string {
		if s == old {
			return new
		}
		return s
	},
	"trimprefix": func(s, prefix string) string {
		return strings.TrimPrefix(s, prefix)
	},
}

// installCmds maps an install tool name to a Dockerfile RUN template.
var installCmds = map[string]string{
	"curl": `RUN {{ range $i, $p := .Platforms -}}
{{ if eq $i 0 }}case "${ARCH}" in \
{{ end }}        {{ $p.Arch }}) CHECKSUM="{{ $p.Checksum }}" ;; \
{{ end }}        *) echo "Unsupported: ${ARCH}"; exit 1 ;; \
    esac && \
    export TMP_DIR=$(mktemp -d) && \
    export TMP_FILE="${TMP_DIR}/{{ .Name }}.tar.gz" && \
    case "${ARCH}" in \
{{ range .Platforms }}        {{ .Arch }}) DOWNLOAD_URL="{{ .DownloadURL }}"; EXTRACT="{{ .Extract }}" ;; \
{{ end }}    esac && \
    curl -fsSL "${DOWNLOAD_URL}" > "${TMP_FILE}" && \
    printf "%s  %s\n" "${CHECKSUM}" "${TMP_FILE}" > "${TMP_DIR}/checksum.sha256" && \
    sha256sum -c "${TMP_DIR}/checksum.sha256" && \
    tar xzf "${TMP_FILE}" -C "${TMP_DIR}" && \
    install "${TMP_DIR}/${EXTRACT}" /usr/local/bin/ && \
    rm -rf "${TMP_DIR}"`,

	"curl-bin": `RUN {{ range $i, $p := .Platforms -}}
{{ if eq $i 0 }}case "${ARCH}" in \
{{ end }}        {{ $p.Arch }}) CHECKSUM="{{ $p.Checksum }}" ;; \
{{ end }}        *) echo "Unsupported: ${ARCH}"; exit 1 ;; \
    esac && \
    export TMP_DIR=$(mktemp -d) && \
    export TMP_FILE="${TMP_DIR}/{{ .Name }}" && \
    case "${ARCH}" in \
{{ range .Platforms }}        {{ .Arch }}) DOWNLOAD_URL="{{ .DownloadURL }}" ;; \
{{ end }}    esac && \
    curl -fsSL "${DOWNLOAD_URL}" > "${TMP_FILE}" && \
    printf "%s  %s\n" "${CHECKSUM}" "${TMP_FILE}" > "${TMP_DIR}/checksum.sha256" && \
    sha256sum -c "${TMP_DIR}/checksum.sha256" && \
    install "${TMP_FILE}" /usr/local/bin/ && \
    rm -rf "${TMP_DIR}"`,

	"go-install": `RUN go install {{ .InstallPackage }}`,
}

var sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// sortedPlatforms returns the keys of a checksums map sorted alphabetically.
func sortedPlatforms(checksums map[string]string) []string {
	return slices.Sorted(maps.Keys(checksums))
}

// renderTemplate parses and executes a Go template string with the given data.
func renderTemplate(name, tmplStr string, funcs template.FuncMap, data any) (string, error) {
	t, err := template.New(name).Funcs(funcs).Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// discoverTemplates returns a list of all .tmpl files in the templates directory.
func discoverTemplates() ([]string, error) {
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		return nil, fmt.Errorf("reading templates dir: %w", err)
	}
	var templates []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), templateSuffix) {
			templates = append(templates, e.Name())
		}
	}
	return templates, nil
}

// validateTools checks that each tool has the required fields populated.
func validateTools(tools []Tool) error {
	for i, t := range tools {
		if t.Name == "" {
			return fmt.Errorf("tool at index %d is missing required field 'name'", i)
		}
		if t.InstallTool == "" {
			return fmt.Errorf("tool %s is missing required field 'install_tool'", t.Name)
		}
		if len(t.Dockerfiles) == 0 {
			return fmt.Errorf("tool %s is missing required field 'dockerfiles'", t.Name)
		}
	}
	return nil
}

// renderToolBlock renders the Dockerfile RUN block for a single tool.
func renderToolBlock(t Tool) (string, error) {
	baseVars := TemplateVars{
		Source:        t.Source,
		Version:       t.Version,
		VersionCommit: t.VersionCommit,
	}

	// Build per-platform resolved values for curl-based tools in sorted order.
	var platforms []PlatformResolved
	for _, platform := range sortedPlatforms(t.Checksums) {
		checksum := t.Checksums[platform]

		parts := strings.SplitN(platform, "/", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid platform format %q for tool %s, expected os/arch", platform, t.Name)
		}

		if !sha256Re.MatchString(checksum) {
			return "", fmt.Errorf("invalid SHA-256 checksum %q for tool %s platform %s", checksum, t.Name, platform)
		}

		vars := baseVars
		vars.OS = parts[0]
		vars.Arch = parts[1]

		dlURL, err := renderTemplate("", t.DownloadTemplate, funcMap, vars)
		if err != nil {
			return "", fmt.Errorf("download_template for %s: %w", t.Name, err)
		}
		extract, err := renderTemplate("", t.Extract, funcMap, vars)
		if err != nil {
			return "", fmt.Errorf("extract for %s: %w", t.Name, err)
		}

		platforms = append(platforms, PlatformResolved{
			Arch:        parts[1],
			DownloadURL: dlURL,
			Extract:     extract,
			Checksum:    checksum,
		})
	}

	if t.InstallTool == "curl" && len(platforms) == 0 {
		return "", fmt.Errorf("tool %s uses curl but has no checksums defined", t.Name)
	}

	// Resolve install_template for go-install tools.
	var installPackage string
	if t.InstallTemplate != "" {
		resolved, err := renderTemplate("", t.InstallTemplate, funcMap, baseVars)
		if err != nil {
			return "", fmt.Errorf("install_template for %s: %w", t.Name, err)
		}
		installPackage = resolved
	}

	if t.InstallTool == "go-install" && installPackage == "" {
		return "", fmt.Errorf("tool %s uses go-install but has no install_template defined", t.Name)
	}

	cmdTmpl, ok := installCmds[t.InstallTool]
	if !ok {
		return "", fmt.Errorf("unknown install_tool %q for %s", t.InstallTool, t.Name)
	}

	dockerVars := DockerfileToolVars{
		Name:           t.Name,
		InstallPackage: installPackage,
		Platforms:      platforms,
	}
	return renderTemplate("dockerfile", cmdTmpl, nil, dockerVars)
}

// templateNameForDockerfile returns the template filename for a dockerfile value.
// "all" is handled by the caller; specific values like "go1.25" map to "Dockerfile.go1.25.tmpl".
func templateNameForDockerfile(value string) string {
	return fmt.Sprintf("Dockerfile.%s%s", value, templateSuffix)
}

// toolAppliesToTemplate checks if a tool should be injected into the given template file.
func toolAppliesToTemplate(t Tool, tmplName string) bool {
	for _, df := range t.Dockerfiles {
		if df == "all" {
			return true
		}
		if templateNameForDockerfile(df) == tmplName {
			return true
		}
	}
	return false
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// 1. Read deps.yaml.
	data, err := os.ReadFile("deps.yaml")
	if err != nil {
		return fmt.Errorf("reading deps.yaml: %w", err)
	}

	var deps Deps
	if err := yaml.Load(data, &deps); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	// 2. Validate required fields for each tool.
	if err := validateTools(deps.Tools); err != nil {
		return err
	}

	// 3. Discover all template files in templates/.
	allTemplates, err := discoverTemplates()
	if err != nil {
		return err
	}
	if len(allTemplates) == 0 {
		return fmt.Errorf("no template files found in %s/", templatesDir)
	}

	// 4. Ensure output directory exists.
	if err := os.MkdirAll(dockerfilesDir, 0o755); err != nil {
		return fmt.Errorf("creating output dir %s: %w", dockerfilesDir, err)
	}

	// 5. For each template, collect matching tool blocks and render.
	for _, tmplName := range allTemplates {
		tmplPath := filepath.Join(templatesDir, tmplName)

		tmplContent, err := os.ReadFile(tmplPath)
		if err != nil {
			return fmt.Errorf("reading template %s: %w", tmplPath, err)
		}

		if !strings.Contains(string(tmplContent), dependenciesMarker) {
			return fmt.Errorf("template %s is missing the %q marker", tmplPath, dependenciesMarker)
		}

		// Collect rendered RUN blocks for tools that target this template.
		var blocks []string
		hasGoInstall := false
		for _, t := range deps.Tools {
			if !toolAppliesToTemplate(t, tmplName) {
				continue
			}

			block, err := renderToolBlock(t)
			if err != nil {
				return fmt.Errorf("rendering tool %s: %w", t.Name, err)
			}
			blocks = append(blocks, fmt.Sprintf("# %s %s\n%s", t.Name, t.Version, block))

			if t.InstallTool == "go-install" {
				hasGoInstall = true
			}
		}

		// Append go cache cleanup if any go-install tool was injected.
		if hasGoInstall {
			blocks = append(blocks, "# Cleanup Go caches\nRUN go clean -cache -modcache")
		}

		// Replace the marker with the collected blocks.
		replacement := strings.Join(blocks, "\n\n")
		output := strings.ReplaceAll(string(tmplContent), dependenciesMarker, replacement)

		// Write the rendered file to dockerfiles/, removing the .tmpl suffix.
		outputName := filepath.Base(strings.TrimSuffix(tmplName, templateSuffix))
		outputPath := filepath.Join(dockerfilesDir, outputName)
		if err := os.WriteFile(outputPath, []byte(output), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", outputPath, err)
		}

		log.Printf("Generated %s from %s - %d tool(s) injected", outputPath, tmplPath, len(blocks))
	}

	return nil
}
