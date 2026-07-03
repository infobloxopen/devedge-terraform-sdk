// Command tfkit-doctor reports the tfkit environment for building and running a
// generated Terraform provider: Go toolchain, tfkit version, and the pinned
// framework code generator tfgen shells out to. It mirrors clikit-doctor in
// devedge-cli-sdk.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/infobloxopen/devedge-terraform-sdk/internal/gen"
	"github.com/infobloxopen/devedge-terraform-sdk/tfkit"
)

func main() {
	flag.Parse()
	if err := doctor(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func doctor(out *os.File) error {
	fmt.Fprintf(out, "tfkit:          %s\n", tfkit.Version)
	fmt.Fprintf(out, "go:             %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	if path, err := exec.LookPath("go"); err == nil {
		fmt.Fprintf(out, "go on PATH:     %s\n", path)
	} else {
		fmt.Fprintf(out, "go on PATH:     NOT FOUND (required to generate a provider)\n")
	}
	fmt.Fprintf(out, "framework gen:  %s@%s\n", gen.FrameworkGenModule, gen.FrameworkGenVersion)
	return nil
}
