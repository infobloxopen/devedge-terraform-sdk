# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `tfkit` — the provider runtime library: an authed `Client` (bearer + JSON
  `DoCreate`/`DoRead`/`DoUpdate`/`DoDelete` helpers), a reusable
  `provider.Provider` base (`NewProvider`) that configures a shared client from
  `endpoint`/`token` (with `<PROVIDER>_ENDPOINT`/`<PROVIDER>_TOKEN` environment
  fallbacks), model↔wire mapping helpers (`SetString`/`GetInt64`/…, `MaskKeys`),
  an `ImportStatePassthroughID` helper, and a generic long-running-operation
  poll.
- `tfkit/behavior` — the framework-free `field_behavior`→Terraform schema
  semantics: `REQUIRED`→required, `OUTPUT_ONLY`→computed (+ `UseStateForUnknown`),
  `IMMUTABLE`→`RequiresReplace`, `INPUT_ONLY`/secret→sensitive, and the rule that
  storage `not_null` is never mapped to required.
- `cmd/tfgen` — a generator that turns an enriched OpenAPI v3 spec into a
  `terraform-plugin-framework` provider. It emits the HashiCorp Provider Code
  Specification directly (because `tfplugingen-openapi` ignores `field_behavior`),
  runs the pinned `tfplugingen-framework@v0.4.1` to produce schema+models, then
  templates the CRUD glue and resource registration. Honors `field_behavior`
  (`REQUIRED`, `OUTPUT_ONLY`, `IMMUTABLE`, `INPUT_ONLY`/secret) and `enum`.
- `cmd/tfkit-doctor` — an environment/diagnostics binary.
- `examples/terraform-provider-toy` — a runnable provider generated from the toy
  fixture, with committed generated output, a schema-assertion test
  (Required/Computed/RequiresReplace/Sensitive/enum-validator), and a CRUD test
  that drives `Create`/`Read` against a fake service through `tfkit`.

### Notes

- Terraform write-only attributes (`schema.WriteOnly`) are not emitted: the
  pinned `terraform-plugin-codegen-spec` v0.2.0 has no `write_only` field, so the
  generator sets `Sensitive` instead. See [SECURITY.md](./SECURITY.md).
