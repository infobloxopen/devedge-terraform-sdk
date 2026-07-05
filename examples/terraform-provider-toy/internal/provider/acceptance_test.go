package provider

import (
	"fmt"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestAccWidgetServerSetIdentityCleanApply is the acceptance test for issue #7:
// a resource whose identity (id) and tenant key (account_id) are populated by
// the server, with the client omitting both. It drives a real
// `terraform apply` (and, in the second step, a real `terraform plan`) through
// the generated provider against an in-process fake service.
//
// Before the fix, tfgen mapped id and account_id to plain `optional`, so the
// server-supplied values tripped Terraform's post-apply consistency check with
// "Provider produced inconsistent result after apply". With id/account_id
// resolved to computed_optional + UseStateForUnknown, the apply completes
// cleanly and a re-plan is empty (no perpetual diff).
//
// This test only runs under `TF_ACC=1` (the terraform-plugin-testing gate) with
// a real Terraform binary on PATH or at TF_ACC_TERRAFORM_PATH; it is skipped in
// the standard `go test ./...` unit run, including the repo's CI, which does not
// install Terraform. The generator/spec assertions in schema_test.go and
// internal/gen exercise the same mapping without a Terraform binary.
func TestAccWidgetServerSetIdentityCleanApply(t *testing.T) {
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	config := fmt.Sprintf(`
provider "toy" {
  endpoint = %q
  token    = "test-token"
}

resource "toy_widget" "test" {
  display_name = "Made By TF"
  category     = "standard"
  color        = "red"
}
`, srv.URL)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"toy": providerserver.NewProtocol6WithError(New("acc")()),
		},
		Steps: []resource.TestStep{
			{
				// Clean plan + apply: the server sets id and account_id (the
				// client omits both). This is the exact path that failed with
				// "inconsistent result after apply" before the fix.
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("toy_widget.test", "id"),
					resource.TestCheckResourceAttrSet("toy_widget.test", "account_id"),
					resource.TestCheckResourceAttr("toy_widget.test", "display_name", "Made By TF"),
					resource.TestMatchResourceAttr("toy_widget.test", "name", regexp.MustCompile(`^widgets/`)),
				),
			},
			{
				// Re-plan the unchanged config: the server-populated identity
				// must not produce a perpetual diff (UseStateForUnknown holds it
				// stable across plans).
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}
