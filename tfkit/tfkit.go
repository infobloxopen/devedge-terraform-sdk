// Package tfkit is the Terraform runtime library that a generated devedge
// provider imports — the Terraform mirror of clikit in devedge-cli-sdk. It is
// small, public, and mechanism-only: it carries the heavy
// terraform-plugin-framework dependency so a generated provider does not have
// to wire the plumbing by hand, and so that weight stays isolated to this one
// module.
//
// tfkit provides:
//
//   - [Client] — an authed JSON HTTP client the generated resources call
//     directly against the service's REST surface (no generated Go client),
//     the Terraform analog of how clikit uses net/http.
//   - [NewProvider] — a reusable [provider.Provider] base (endpoint/token
//     configuration + a Configure that builds and shares the [Client]) that a
//     generated provider composes.
//   - model↔wire mapping helpers ([SetString]/[GetString]/…) that bridge the
//     framework's types.String/Int64/Bool values and plain JSON, plus a generic
//     CRUD helper set on [Client] (DoCreate/DoRead/DoUpdate/DoDelete).
//   - [ImportStatePassthroughID] and [PollOperation], the import and
//     long-running-operation helpers a generated resource needs.
//
// The field-behavior→schema semantics that keep a generated schema consistent
// live in the framework-free sub-package tfkit/behavior, so the generator can
// import them without pulling this runtime into its dependency graph.
package tfkit

// Version is the tfkit runtime version, surfaced by tfkit-doctor. It tracks the
// module's released tag.
const Version = "0.1.0"
