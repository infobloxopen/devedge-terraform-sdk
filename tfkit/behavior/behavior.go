// Package behavior maps an enriched API contract's field-behavior signals to
// Terraform schema semantics. It is intentionally framework-free — it imports
// no terraform-plugin-framework runtime and no codegen-spec types — so the
// tfgen generator can depend on it for the spec-emit path without widening its
// dependency graph. It is the single source of truth for "what does REQUIRED /
// OUTPUT_ONLY / IMMUTABLE / INPUT_ONLY mean for a Terraform attribute", shared
// by the generator and documented for provider authors.
package behavior

// Field-behavior tokens, matching google.api.field_behavior values as they
// surface in the enriched OpenAPI x-aip-field-behavior extension.
const (
	// Required marks a client-required input → a `required` attribute.
	Required = "REQUIRED"
	// OutputOnly marks a server-set field → a `computed` attribute.
	OutputOnly = "OUTPUT_ONLY"
	// InputOnly marks write-only material (a secret) → a `sensitive` attribute.
	InputOnly = "INPUT_ONLY"
	// Immutable marks a set-once field → a RequiresReplace plan modifier.
	Immutable = "IMMUTABLE"
)

// AccountKey is the conventional tenant-key attribute name. Like the resource
// identity, it is server-populated (set from the auth context) and immutable,
// so the generator treats it as identity for Terraform semantics (see
// [Resolve]'s identity rule).
const AccountKey = "account_id"

// Disposition is the ComputedOptionalRequired value of a Provider Code
// Specification attribute.
type Disposition string

const (
	// Required attribute: must be set in configuration.
	DispRequired Disposition = "required"
	// Optional attribute: may be set in configuration.
	DispOptional Disposition = "optional"
	// Computed attribute: set by the provider, never in configuration.
	DispComputed Disposition = "computed"
	// ComputedOptional attribute: may be set, otherwise the provider supplies it.
	DispComputedOptional Disposition = "computed_optional"
)

// Semantics is the Terraform schema disposition derived from field behavior.
// It is plain data — the generator turns it into codegen-spec structs.
type Semantics struct {
	// Disposition is the required/optional/computed classification.
	Disposition Disposition
	// RequiresReplace adds a RequiresReplace() plan modifier (IMMUTABLE).
	RequiresReplace bool
	// UseStateForUnknown adds a UseStateForUnknown() plan modifier to keep a
	// computed value stable across plans.
	UseStateForUnknown bool
	// Sensitive marks the attribute value as sensitive (INPUT_ONLY/secret).
	Sensitive bool
}

// Resolve maps native OpenAPI signals (required membership, readOnly,
// writeOnly) plus explicit x-aip-field-behavior tokens to Terraform schema
// [Semantics]. The identity flag marks the resource identity (the resource key,
// e.g. id) and the tenant key ([AccountKey]) — fields the server populates when
// the client omits them.
//
// The rules, in order:
//
//   - OUTPUT_ONLY / readOnly wins → computed (+ UseStateForUnknown to stabilize
//     the value across plans). A computed field is never required or optional.
//   - otherwise REQUIRED / native-required → required.
//   - otherwise identity → computed_optional (+ UseStateForUnknown). The client
//     may set the value, but the server supplies it when omitted; a plain
//     optional attribute whose value the server fills in makes `terraform apply`
//     fail with "Provider produced inconsistent result after apply". Marking it
//     computed_optional lets Terraform tolerate the server-populated value and
//     UseStateForUnknown holds it stable across plans.
//   - otherwise → optional.
//   - IMMUTABLE (and not output-only) keeps the disposition and adds
//     RequiresReplace: the field is set once and changing it forces a new
//     resource. This composes with the identity rule — an immutable identity is
//     computed_optional + UseStateForUnknown + RequiresReplace.
//   - INPUT_ONLY / writeOnly → sensitive.
//
// Storage-only signals such as not_null are deliberately NOT inputs here: a
// column being NOT NULL never makes a client field required.
func Resolve(behaviors []string, nativeRequired, readOnly, writeOnly, identity bool) Semantics {
	set := make(map[string]bool, len(behaviors))
	for _, b := range behaviors {
		set[b] = true
	}
	outputOnly := readOnly || set[OutputOnly]
	inputOnly := writeOnly || set[InputOnly]
	required := nativeRequired || set[Required]

	var s Semantics
	switch {
	case outputOnly:
		s.Disposition = DispComputed
		s.UseStateForUnknown = true
	case required:
		s.Disposition = DispRequired
	case identity:
		s.Disposition = DispComputedOptional
		s.UseStateForUnknown = true
	default:
		s.Disposition = DispOptional
	}
	if set[Immutable] && !outputOnly {
		s.RequiresReplace = true
	}
	if inputOnly {
		s.Sensitive = true
	}
	return s
}
