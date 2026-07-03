// Command tfgen turns an enriched OpenAPI v3 spec into a
// terraform-plugin-framework provider that imports the tfkit runtime. It is the
// Terraform-side analog of cligen in devedge-cli-sdk; de terraform add runs it.
//
// tfgen emits the HashiCorp Provider Code Specification itself — with the
// enriched field_behavior semantics set explicitly — then drives the pinned
// tfplugingen-framework to produce schema+models, and finally templates the
// CRUD glue and resource registration.
//
// Usage:
//
//	tfgen --input <spec> --output <provider-internal-dir> \
//	      --module <provider module path> --provider <name> [--resource <name>]
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/infobloxopen/devedge-terraform-sdk/internal/gen"
)

func main() {
	var (
		input    = flag.String("input", "", "path to the enriched OpenAPI v3 spec (required)")
		output   = flag.String("output", "", "provider-internal output directory (required)")
		module   = flag.String("module", "", "provider go module path")
		provider = flag.String("provider", "", "provider type name (required, e.g. \"toy\")")
		pkg      = flag.String("package", "provider", "generated Go package name")
		resource = flag.String("resource", "", "limit generation to this resource (TF name); empty = all")
	)
	flag.Parse()

	if err := run(*input, *output, *module, *provider, *pkg, *resource); err != nil {
		fmt.Fprintln(os.Stderr, "tfgen:", err)
		os.Exit(1)
	}
}

func run(input, output, module, provider, pkg, resource string) error {
	// Preflight: fail loud before touching the filesystem.
	if input == "" {
		return fmt.Errorf("--input is required")
	}
	if output == "" {
		return fmt.Errorf("--output is required")
	}
	if provider == "" {
		return fmt.Errorf("--provider is required")
	}
	if _, err := os.Stat(input); err != nil {
		return fmt.Errorf("--input spec not readable: %w", err)
	}

	written, err := gen.Generate(gen.Options{
		SpecPath:  input,
		OutputDir: output,
		Module:    module,
		Provider:  provider,
		Package:   pkg,
		Resource:  resource,
	})
	if err != nil {
		return err
	}
	for _, f := range written {
		fmt.Println("wrote", f)
	}
	return nil
}
