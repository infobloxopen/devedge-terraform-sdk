package gen

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Pinned generator toolchain. tfgen runs the HashiCorp framework code generator
// via `go run <module>@<version>` so the exact version is reproducible and no
// separate install step is required. This is the ONLY code-emitting tool tfgen
// shells out to; the Provider Code Specification is emitted in-process.
const (
	FrameworkGenModule  = "github.com/hashicorp/terraform-plugin-codegen-framework/cmd/tfplugingen-framework"
	FrameworkGenVersion = "v0.4.1"
)

// SpecFileName is the Provider Code Specification tfgen emits into the output
// directory before running the framework generator.
const SpecFileName = "provider_code_spec.json"

// RegistrationFileName is the generated resource-registration file.
const RegistrationFileName = "resources_gen.go"

// Options configures a generation run.
type Options struct {
	SpecPath  string // path to the enriched OpenAPI v3 spec
	OutputDir string // provider-internal directory to emit into (e.g. ./internal/provider)
	Module    string // provider go module path (informational)
	Provider  string // provider type name (e.g. "toy")
	Package   string // generated Go package name (default "provider")
	Resource  string // limit generation to this resource (TF name); empty = all
}

// Generate parses the enriched spec, emits the Provider Code Specification,
// runs the pinned tfplugingen-framework to produce schema+models, then templates
// the CRUD glue and registration. It returns the sorted list of written files.
func Generate(opts Options) ([]string, error) {
	if opts.SpecPath == "" || opts.OutputDir == "" {
		return nil, fmt.Errorf("SpecPath and OutputDir are required")
	}
	if opts.Package == "" {
		opts.Package = "provider"
	}
	data, err := os.ReadFile(opts.SpecPath)
	if err != nil {
		return nil, fmt.Errorf("read spec: %w", err)
	}
	var probe map[string]any
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("spec is not valid YAML/JSON: %w", err)
	}

	model, err := Parse(data, opts.Module, opts.Package, opts.Provider)
	if err != nil {
		return nil, err
	}
	if opts.Resource != "" {
		model, err = filterResource(model, opts.Resource)
		if err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, err
	}

	// 1. Emit the Provider Code Specification JSON with enriched semantics.
	specJSON, err := EmitSpecJSON(model)
	if err != nil {
		return nil, err
	}
	specPath := filepath.Join(opts.OutputDir, SpecFileName)
	if err := os.WriteFile(specPath, specJSON, 0o644); err != nil {
		return nil, err
	}

	// 2. Run the pinned framework generator on the spec to emit schema+models.
	if err := runFrameworkGen(specPath, opts.OutputDir, opts.Package); err != nil {
		return nil, err
	}

	// 3. Template the CRUD glue + registration.
	files, err := renderGlue(model)
	if err != nil {
		return nil, err
	}
	for rel, content := range files {
		dst := filepath.Join(opts.OutputDir, rel)
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return nil, err
		}
	}

	written := []string{specPath}
	for rel := range files {
		written = append(written, filepath.Join(opts.OutputDir, rel))
	}
	// The framework generator's *_gen.go files:
	for _, r := range model.Resources {
		written = append(written, filepath.Join(opts.OutputDir, r.TFName+"_resource_gen.go"))
	}
	sort.Strings(written)
	return written, nil
}

// runFrameworkGen preflights `go` and runs the pinned tfplugingen-framework.
func runFrameworkGen(specPath, outputDir, pkg string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("`go` not found on PATH (needed to run %s): %w", FrameworkGenModule, err)
	}
	toolRef := FrameworkGenModule + "@" + FrameworkGenVersion
	cmd := exec.Command("go", "run", toolRef, "generate", "resources",
		"--input", specPath, "--output", outputDir, "--package", pkg)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tfplugingen-framework failed: %w\n%s", err, stderr.String())
	}
	return nil
}

func filterResource(m *Model, tfName string) (*Model, error) {
	for _, r := range m.Resources {
		if r.TFName == tfName {
			cp := *m
			cp.Resources = []Resource{r}
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("resource %q not found in spec", tfName)
}

// renderGlue renders one <tfname>_resource.go of CRUD glue per resource plus
// the shared registration file, keyed by path relative to the output directory.
// One glue file per resource mirrors the framework generator's
// one-file-per-resource layout and keeps output stable.
func renderGlue(m *Model) (map[string]string, error) {
	files := map[string]string{}
	for _, r := range m.Resources {
		single := &Model{Module: m.Module, Package: m.Package, Provider: m.Provider, Resources: []Resource{r}}
		src, err := execGo(glueTmpl, buildView(single))
		if err != nil {
			return nil, fmt.Errorf("render glue for %s: %w", r.TFName, err)
		}
		files[r.TFName+"_resource.go"] = src
	}
	reg, err := execGo(registrationTmpl, buildView(m))
	if err != nil {
		return nil, fmt.Errorf("render registration: %w", err)
	}
	files[RegistrationFileName] = reg
	return files, nil
}

func buildView(m *Model) tmplModel {
	view := tmplModel{Package: m.Package, Provider: m.Provider, Module: m.Module}
	for _, r := range m.Resources {
		tr := tmplResource{
			GoName:          r.GoName,
			TFName:          r.TFName,
			ResourceType:    r.ResourceType,
			ModelType:       r.ModelType(),
			SchemaFunc:      r.SchemaFunc(),
			StructName:      lowerFirst(r.GoName) + "Resource",
			ConstructorName: "New" + r.GoName + "Resource",
			CollectionConst: lowerFirst(r.GoName) + "CollectionPath",
			CollectionPath:  r.CollectionPath,
			Key:             r.Key,
			KeyGoName:       pascal(r.Key),
			HasCreate:       r.Has("Create"),
			HasRead:         r.Has("Get"),
			HasUpdate:       r.Has("Update"),
			HasDelete:       r.Has("Delete"),
		}
		tr.NeedApply = tr.HasCreate || tr.HasRead || tr.HasUpdate
		if tr.HasRead || tr.HasUpdate || tr.HasDelete {
			view.NeedURL = true
		}
		if tr.HasUpdate {
			view.NeedString = true
		}
		for _, f := range r.Fields {
			tf := tmplField{GoName: f.GoName, JSONName: f.JSONName, Setter: setterFor(f.TFType), Getter: getterFor(f.TFType)}
			if f.Writable() {
				tr.CreateFields = append(tr.CreateFields, tf)
			}
			if f.Mutable() {
				tr.UpdateFields = append(tr.UpdateFields, tf)
			}
			if !f.InputOnly {
				tr.ApplyFields = append(tr.ApplyFields, tf)
			}
		}
		view.Resources = append(view.Resources, tr)
	}
	return view
}

func setterFor(tfType string) string {
	switch tfType {
	case "int64":
		return "SetInt64"
	case "bool":
		return "SetBool"
	case "float64":
		return "SetFloat64"
	default:
		return "SetString"
	}
}

func getterFor(tfType string) string {
	switch tfType {
	case "int64":
		return "GetInt64"
	case "bool":
		return "GetBool"
	case "float64":
		return "GetFloat64"
	default:
		return "GetString"
	}
}

func execGo(tmpl string, data any) (string, error) {
	raw, err := execRaw(tmpl, data)
	if err != nil {
		return "", err
	}
	formatted, err := format.Source([]byte(raw))
	if err != nil {
		return "", fmt.Errorf("gofmt generated source: %w\n---\n%s", err, raw)
	}
	return string(formatted), nil
}

func execRaw(tmpl string, data any) (string, error) {
	t, err := template.New("gen").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
