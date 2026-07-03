# Security

## Reporting

Report suspected vulnerabilities via a private security advisory on
[the repository](https://github.com/infobloxopen/devedge-terraform-sdk/security/advisories),
not a public issue.

## Runtime posture

- **Token scoping.** The bearer token is attached by the `tfkit.Client`
  transport to requests the provider makes against the configured `endpoint`.
  Point a provider only at endpoints you trust with the token.
- **Token sourcing.** The provider reads `token` from the configuration block or
  the `<PROVIDER>_TOKEN` environment variable. Prefer the environment (or a
  secrets manager) over committing tokens to `.tf` files. The `token` attribute
  is marked `Sensitive`, so Terraform redacts it from plan/apply output.
- **Write-only material.** Fields marked `INPUT_ONLY`/secret in the contract are
  generated as `Sensitive` attributes and are **excluded from the
  response-apply path**: the service does not echo them back, so the provider
  keeps the value the configuration supplied rather than overwriting it with
  null. See the "WriteOnly" note below for the toolchain limitation on the
  newer Terraform write-only attribute feature.
- **No secrets in generated code.** `tfgen` never bakes an endpoint, token, or
  identity-provider name into generated source; those come from configuration
  or the environment at runtime.

## WriteOnly attributes

Terraform's newer write-only attribute feature (schema `WriteOnly`, which keeps
a value out of state entirely) is **not** applied by this generator. The pinned
`terraform-plugin-codegen-spec` (v0.2.0) has no `write_only` field on its
attribute types — only `sensitive` — so `tfplugingen-framework` cannot round-trip
it. Secret/`INPUT_ONLY` fields are therefore generated as `Sensitive` (redacted
in output) and preserved from configuration rather than read back. When the spec
schema gains a `write_only` field, the generator can set it directly.

## Dependency posture

The `tfkit` runtime depends on `terraform-plugin-framework` (and, when served,
the standard provider plugin stack: `terraform-plugin-go`, `go-plugin`, gRPC).
That weight is **intentionally isolated to this module** — a devedge service
repo does not import it, so this SDK does not widen a service's dependency
graph. The generator's spec-emit path (`internal/gen` → `tfkit/behavior` +
`terraform-plugin-codegen-spec` + `kin-openapi`) is free of the Terraform
runtime.

`tfgen` shells out to the pinned framework code generator via
`go run github.com/hashicorp/terraform-plugin-codegen-framework/cmd/tfplugingen-framework@v0.4.1`
so the exact generator version is reproducible and no separate install step
runs untrusted code outside the module graph.
