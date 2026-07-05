package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/infobloxopen/devedge-terraform-sdk/tfkit"
)

// fakeWidgets is a minimal in-memory Widget REST API the generated resource
// drives through tfkit. It records the bearer token and strips the write-only
// secretToken from responses (as a real service would), so the test proves the
// generated apply() preserves INPUT_ONLY material from the plan.
type fakeWidgets struct {
	mu         sync.Mutex
	store      map[string]map[string]any
	seenBearer string
	nextID     int
}

func newFake() *fakeWidgets {
	return &fakeWidgets{store: map[string]map[string]any{}}
}

func (f *fakeWidgets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		f.seenBearer = strings.TrimPrefix(h, "Bearer ")
	}
	w.Header().Set("Content-Type", "application/json")

	id := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/v1/widgets"), "/")

	switch {
	case r.Method == http.MethodPost && id == "":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.nextID++
		newID, _ := body["id"].(string)
		if newID == "" {
			newID = "srv" + string(rune('0'+f.nextID))
		}
		body["id"] = newID
		body["name"] = "widgets/" + newID
		// The service sets the tenant key from the auth context when the client
		// omits it — the account_id case from issue #7.
		if acct, _ := body["accountId"].(string); acct == "" {
			body["accountId"] = "acct-" + newID
		}
		delete(body, "secretToken") // service strips write-only material
		f.store[newID] = body
		_ = json.NewEncoder(w).Encode(body)
	case r.Method == http.MethodGet && id != "":
		v, ok := f.store[id]
		if !ok {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(v)
	case r.Method == http.MethodDelete && id != "":
		delete(f.store, id)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	default:
		http.Error(w, `{"message":"unhandled"}`, http.StatusBadRequest)
	}
}

func TestWidgetCreateReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	s := WidgetResourceSchema(ctx)
	r := NewWidgetResource().(*widgetResource)

	// Configure injects the tfkit client, exactly as the provider does at runtime.
	cfgResp := &resource.ConfigureResponse{}
	r.Configure(ctx, resource.ConfigureRequest{ProviderData: tfkit.NewClient(srv.URL, "test-token")}, cfgResp)
	if cfgResp.Diagnostics.HasError() {
		t.Fatalf("Configure diagnostics: %v", cfgResp.Diagnostics)
	}

	// Create with a plan carrying a required field, an enum, and secret material.
	plan := tfsdk.Plan{Schema: s}
	if d := plan.Set(ctx, WidgetModel{
		DisplayName: types.StringValue("Made By TF"),
		Category:    types.StringValue("standard"),
		SecretToken: types.StringValue("s3cr3t"),
		Color:       types.StringValue("red"),
	}); d.HasError() {
		t.Fatalf("plan.Set: %v", d)
	}

	createResp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("Create diagnostics: %v", createResp.Diagnostics)
	}

	var created WidgetModel
	if d := createResp.State.Get(ctx, &created); d.HasError() {
		t.Fatalf("read created state: %v", d)
	}
	if created.DisplayName.ValueString() != "Made By TF" {
		t.Fatalf("display_name did not round-trip: %q", created.DisplayName.ValueString())
	}
	if created.Id.ValueString() == "" {
		t.Fatalf("server-assigned id was not applied to state")
	}
	if created.Name.ValueString() != "widgets/"+created.Id.ValueString() {
		t.Fatalf("computed name not applied: %q", created.Name.ValueString())
	}
	// INPUT_ONLY secret is stripped by the service but preserved from the plan.
	if created.SecretToken.ValueString() != "s3cr3t" {
		t.Fatalf("secret_token should be preserved from plan, got %q", created.SecretToken.ValueString())
	}

	// Read the created resource back.
	readResp := &resource.ReadResponse{State: tfsdk.State{Schema: s}}
	r.Read(ctx, resource.ReadRequest{State: createResp.State}, readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("Read diagnostics: %v", readResp.Diagnostics)
	}
	var read WidgetModel
	if d := readResp.State.Get(ctx, &read); d.HasError() {
		t.Fatalf("read state: %v", d)
	}
	if read.DisplayName.ValueString() != "Made By TF" || read.Color.ValueString() != "red" {
		t.Fatalf("read did not round-trip: display_name=%q color=%q", read.DisplayName.ValueString(), read.Color.ValueString())
	}

	if fake.seenBearer != "test-token" {
		t.Fatalf("service never saw the expected bearer token, saw %q", fake.seenBearer)
	}
}
