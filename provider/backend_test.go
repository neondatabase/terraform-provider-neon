package provider

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	frameworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

// All HTTP, including the generated SDK's fixed Neon origin, is redirected to
// a local server. These tests never contact Neon and need no cloud credentials.
type backendTestHTTP struct {
	client *http.Client
	origin *url.URL
}

func (c backendTestHTTP) Do(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme, clone.URL.Host = c.origin.Scheme, c.origin.Host
	return c.client.Do(clone)
}

type backendTestProvider struct {
	*frameworkProvider
	httpClient neon.HTTPClient
}

// Keep real Terraform lifecycle tests within the repository's unit-test timeout.
// The mock still transitions through building before becoming completed/active.
func (p *backendTestProvider) Resources(ctx context.Context) []func() frameworkresource.Resource {
	factories := p.frameworkProvider.Resources(ctx)
	for i, factory := range factories {
		if _, ok := factory().(*functionResource); ok {
			factories[i] = func() frameworkresource.Resource {
				return &functionResource{pollInterval: time.Millisecond}
			}
		}
	}
	return factories
}

func (p *backendTestProvider) Configure(ctx context.Context, req frameworkprovider.ConfigureRequest, resp *frameworkprovider.ConfigureResponse) {
	p.frameworkProvider.Configure(ctx, req, resp)
	if c, ok := resp.ResourceData.(*neonClient); ok {
		c.sdkCfg.HTTPClient = p.httpClient
	}
}

func backendFactories(server *httptest.Server) map[string]func() (tfprotov6.ProviderServer, error) {
	origin, _ := url.Parse(server.URL)
	return map[string]func() (tfprotov6.ProviderServer, error){"neon": providerserver.NewProtocol6WithError(&backendTestProvider{frameworkProvider: &frameworkProvider{version: "test"}, httpClient: backendTestHTTP{server.Client(), origin}})}
}

func writeTestArchive(t *testing.T, filename, source string) {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, err := w.Create("index.mjs")
	require.NoError(t, err)
	_, err = io.WriteString(f, source)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, os.WriteFile(filename, b.Bytes(), 0600))
}

type backendMock struct {
	t            *testing.T
	mu           sync.Mutex
	exists       bool
	functionID   string
	createdAt    string
	name         string
	version      int
	readCount    int
	env          map[string]string
	deploys      int
	uploads      int
	renamed      int
	deletes      int
	lastPatch    map[string]string
	fail         bool
	auth         bool
	authDB       string
	authProvider string
	authOwner    string
	domains      []neon.CustomDomain
	deleteData   *bool
}

func (m *backendMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer test" {
		m.t.Errorf("missing authorization header")
		w.WriteHeader(401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/projects/p/branches/b")
	functionID := m.functionID
	if functionID == "" {
		functionID = "api"
	}
	createdAt := m.createdAt
	if createdAt == "" {
		createdAt = "2026-10-15T00:00:00Z"
	}
	switch {
	case path == "/functions/api/deployments" && r.Method == "POST":
		if err := r.ParseMultipartForm(34 << 20); err != nil {
			m.t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		if files := r.MultipartForm.File["zip"]; len(files) != 0 {
			m.uploads++
			f, err := files[0].Open()
			if err != nil {
				m.t.Error(err)
				return
			}
			b, err := io.ReadAll(f)
			_ = f.Close()
			if err != nil {
				m.t.Error(err)
				return
			}
			z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
			if err != nil || len(z.File) == 0 || z.File[0].Name != "index.mjs" {
				m.t.Error("invalid root ZIP entry")
			}
		} else if !m.exists {
			w.WriteHeader(400)
			return
		}
		if m.env == nil {
			m.env = map[string]string{}
		}
		m.lastPatch = nil
		if fields := r.MultipartForm.Value["environment"]; len(fields) != 0 {
			if err := json.Unmarshal([]byte(fields[0]), &m.lastPatch); err != nil {
				m.t.Error(err)
				w.WriteHeader(400)
				return
			}
			for k, v := range m.lastPatch {
				if v == "" {
					delete(m.env, k)
				} else {
					m.env[k] = v
				}
			}
		}
		if r.FormValue("runtime") != "nodejs24" {
			m.t.Error("missing runtime")
		}
		m.exists = true
		if m.name == "" {
			m.name = "api"
		}
		m.version++
		m.deploys++
		m.readCount = 0
		w.WriteHeader(201)
		m.json(w, map[string]any{"deployment": map[string]any{"id": m.version, "status": "pending", "runtime": "nodejs24", "memory_mib": 2048, "created_at": "2026-10-15T00:00:00Z"}})
	case path == "/functions/api" && r.Method == "GET":
		if !m.exists {
			m.notFound(w)
			return
		}
		m.readCount++
		status := "completed"
		switch m.readCount {
		case 1:
			status = "pending"
		case 2:
			status = "building"
		}
		if m.fail {
			status = "failed"
		}
		names := []string{}
		for k := range m.env {
			names = append(names, k)
		}
		sort.Strings(names)
		d := map[string]any{"id": m.version, "status": status, "runtime": "nodejs24", "environment": names, "memory_mib": 2048, "created_at": "2026-10-15T00:00:00Z"}
		fn := map[string]any{"id": functionID, "slug": "api", "name": m.name, "created_at": createdAt, "invocation_url": "https://invoke.neon.example/", "current_deployment": d}
		if status == "completed" {
			fn["active_deployment"] = d
		}
		if status == "failed" {
			d["error"] = "synthetic build failure"
		}
		m.json(w, map[string]any{"function": fn})
	case path == "/functions/api" && r.Method == "PATCH":
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			m.t.Error(err)
			return
		}
		m.name = body.Name
		m.renamed++
		m.json(w, map[string]any{"function": map[string]any{"id": functionID, "slug": "api", "name": m.name, "created_at": createdAt, "invocation_url": "https://invoke.neon.example/", "current_deployment": map[string]any{"id": m.version, "status": "completed", "runtime": "nodejs24", "environment": m.names()}, "active_deployment": map[string]any{"id": m.version, "status": "completed", "runtime": "nodejs24"}}})
	case path == "/functions/api" && r.Method == "DELETE":
		m.exists = false
		m.deletes++
		w.WriteHeader(204)
	case path == "/auth" && r.Method == "GET":
		if !m.auth {
			m.notFound(w)
			return
		}
		provider, owner := m.authProvider, m.authOwner
		if provider == "" {
			provider = "better_auth"
		}
		if owner == "" {
			owner = "neon"
		}
		m.json(w, map[string]any{"auth_provider": provider, "auth_provider_project_id": "auth-id", "branch_id": "b", "db_name": m.authDB, "created_at": "2026-10-15T00:00:00Z", "owned_by": owner, "base_url": "https://auth.neon.example/app/auth", "jwks_url": "https://auth.neon.example/app/auth/.well-known/jwks.json"})
	case path == "/auth" && r.Method == "POST":
		var body struct {
			AuthProvider string `json:"auth_provider"`
			DatabaseName string `json:"database_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			m.t.Error(err)
			return
		}
		if body.AuthProvider != "better_auth" || body.DatabaseName == "" {
			m.t.Error("invalid Auth enable request")
		}
		m.auth, m.authDB = true, body.DatabaseName
		w.WriteHeader(201)
		m.json(w, map[string]any{"auth_provider": "better_auth", "auth_provider_project_id": "auth-id", "secret_server_key": "must-not-enter-state", "base_url": "https://auth.neon.example/app/auth", "jwks_url": "https://auth.neon.example/app/auth/.well-known/jwks.json"})
	case path == "/auth" && r.Method == "DELETE":
		var body struct {
			DeleteData *bool `json:"delete_data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			m.t.Error(err)
			return
		}
		m.deleteData = body.DeleteData
		m.auth = false
		w.WriteHeader(200)
	case path == "/custom-domains" && r.Method == "POST":
		var body neon.CustomDomainRegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			m.t.Error(err)
			return
		}
		domain := neon.CustomDomain{Domain: body.Domain, EntityID: body.EntityID, EntityType: body.EntityType, CnameTarget: "exact-regional-edge.neon.example", Status: pointer("pending")}
		m.domains = []neon.CustomDomain{domain}
		w.WriteHeader(201)
		m.json(w, domain)
	case path == "/custom-domains" && r.Method == "GET":
		if r.URL.Query().Get("cursor") == "" {
			m.json(w, map[string]any{"custom_domains": []any{}, "pagination": map[string]any{"next": "opaque+/=&cursor"}})
		} else {
			if r.URL.Query().Get("cursor") != "opaque+/=&cursor" {
				m.t.Error("cursor was not encoded correctly")
			}
			m.json(w, map[string]any{"custom_domains": m.domains})
		}
	case strings.HasPrefix(path, "/custom-domains/") && r.Method == "DELETE":
		m.domains = nil
		w.WriteHeader(204)
	default:
		m.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(500)
	}
}
func (m *backendMock) names() []string {
	names := []string{}
	for key := range m.env {
		names = append(names, key)
	}
	sort.Strings(names)
	return names
}
func (m *backendMock) json(w http.ResponseWriter, value any) {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		m.t.Error(err)
	}
}
func (m *backendMock) notFound(w http.ResponseWriter) {
	w.WriteHeader(404)
	m.json(w, map[string]string{"code": "NOT_FOUND", "message": "not found"})
}

func TestFunctionTerraformLifecycle(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "function.zip")
	writeTestArchive(t, filename, "export default { fetch: () => new Response('one') }")
	m := &backendMock{t: t}
	server := httptest.NewServer(m)
	defer server.Close()
	config := func(name, env string) string {
		return fmt.Sprintf(`provider "neon" { api_key = "test" }
resource "neon_function" "api" {
  project_id = "p"
  branch_id = "b"
  slug = "api"
  name = %q
  zip_file_path = %q
  environment_variables = %s
}`, name, filename, env)
	}
	check := func(deploys, uploads int, env map[string]string) resource.TestCheckFunc {
		return func(state *terraform.State) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.deploys != deploys || m.uploads != uploads || !reflect.DeepEqual(m.env, env) {
				return fmt.Errorf("deploys=%d uploads=%d env=%v; want %d %d %v", m.deploys, m.uploads, m.env, deploys, uploads, env)
			}
			attrs := state.RootModule().Resources["neon_function.api"].Primary.Attributes
			if attrs["current_deployment_status"] != "completed" || attrs["active_deployment_id"] != attrs["current_deployment_id"] {
				return fmt.Errorf("apply returned before deployment activation: %v", attrs)
			}
			if m.deletes != 0 {
				return fmt.Errorf("in-place update deleted function")
			}
			return nil
		}
	}
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: backendFactories(server), CheckDestroy: func(_ *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.exists {
			return fmt.Errorf("function still exists")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: config("API", "{ FOO = \"bar\", REMOVE = \"me\" }"), Check: check(1, 1, map[string]string{"FOO": "bar", "REMOVE": "me"})},
		{PreConfig: func() { writeTestArchive(t, filename, "export default { fetch: () => new Response('two') }") }, Config: config("API", "{ FOO = \"bar\", REMOVE = \"me\" }"), Check: check(2, 2, map[string]string{"FOO": "bar", "REMOVE": "me"})},
		{Config: config("API", "{ FOO = \"baz\" }"), Check: check(3, 2, map[string]string{"FOO": "baz"})},
		{Config: config("API", "{}"), Check: check(4, 2, map[string]string{})},
		{Config: config("Renamed API", "{}"), Check: check(4, 2, map[string]string{})},
		{ResourceName: "neon_function.api", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"zip_file_path", "source_code_hash", "environment_variables"}},
		{PreConfig: func() { m.mu.Lock(); defer m.mu.Unlock(); m.version++; m.env["EXTERNAL"] = "drift"; m.readCount = 3 }, Config: config("Renamed API", "{}"), Check: check(5, 3, map[string]string{})},
		{PreConfig: func() { m.mu.Lock(); defer m.mu.Unlock(); m.createdAt = "2026-10-15T01:00:00.963066Z"; m.readCount = 3 }, Config: config("Renamed API", "{}"), Check: check(6, 4, map[string]string{})},
		{PreConfig: func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.createdAt = "2026-10-15T03:00:00.963066000+02:00"
			m.readCount = 3
		}, Config: config("Renamed API", "{}"), Check: check(6, 4, map[string]string{})},
		{PreConfig: func() { m.mu.Lock(); defer m.mu.Unlock(); m.functionID = "replacement-id"; m.readCount = 3 }, Config: config("Renamed API", "{}"), Check: check(7, 5, map[string]string{})},
		{PreConfig: func() { m.mu.Lock(); defer m.mu.Unlock(); m.exists = false }, Config: config("Renamed API", "{}"), Check: check(8, 6, map[string]string{})},
		{PreConfig: func() { m.mu.Lock(); defer m.mu.Unlock(); m.fail = true }, Config: config("Renamed API", `{ FOO = "after failure" }`), ExpectError: regexp.MustCompile("synthetic build failure")},
		{PreConfig: func() { m.mu.Lock(); defer m.mu.Unlock(); m.fail = false }, Config: config("Renamed API", `{ FOO = "after failure" }`), Check: check(10, 7, map[string]string{"FOO": "after failure"})},
	}})
}

func TestBranchAuthAndDomainTerraformLifecycle(t *testing.T) {
	m := &backendMock{t: t}
	server := httptest.NewServer(m)
	defer server.Close()
	config := `provider "neon" { api_key = "test" }
resource "neon_branch_auth" "app" {
  project_id = "p"
  branch_id = "b"
  database_name = "app"
}
resource "neon_custom_branch_domain" "api" {
  project_id = "p"
  branch_id = "b"
  domain = "api.example.com"
  entity_id = "api"
}`
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: backendFactories(server), CheckDestroy: func(_ *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.auth || len(m.domains) != 0 || m.deleteData == nil || *m.deleteData {
			return fmt.Errorf("destroy did not disable Auth with delete_data=false and remove the domain")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: config, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("neon_branch_auth.app", "auth_provider", "better_auth"), resource.TestCheckResourceAttr("neon_custom_branch_domain.api", "cname_target", "exact-regional-edge.neon.example"))},
		{ResourceName: "neon_branch_auth.app", ImportState: true, ImportStateVerify: true},
		{ResourceName: "neon_custom_branch_domain.api", ImportState: true, ImportStateVerify: true},
		{PreConfig: func() { m.mu.Lock(); defer m.mu.Unlock(); m.domains[0].CnameTarget = "changed-edge.neon.example" }, Config: config, Check: resource.TestCheckResourceAttr("neon_custom_branch_domain.api", "cname_target", "changed-edge.neon.example")},
		{PreConfig: func() { m.mu.Lock(); defer m.mu.Unlock(); m.auth = false; m.domains = nil }, Config: config, Check: resource.TestCheckResourceAttr("neon_branch_auth.app", "database_name", "app")},
	}})
}

func testBackendClient(t *testing.T, handler http.Handler) (*neonClient, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	origin, err := url.Parse(server.URL)
	require.NoError(t, err)
	return &neonClient{sdkCfg: neon.Config{Key: "test", HTTPClient: backendTestHTTP{server.Client(), origin}}}, server
}

func TestFunctionWaitFailureCancellationAndSupersession(t *testing.T) {
	for _, tc := range []struct {
		name   string
		id     int
		status string
		want   string
	}{
		{"failed", 7, "failed", "deployment 7 failed: build exploded"},
		{"superseded", 8, "completed", "superseded"},
		{"timeout", 7, "building", "deadline exceeded"},
		{"completed but not active", 7, "completed", "deadline exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := testBackendClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"function":{"id":"fn","slug":"api","current_deployment":{"id":%d,"status":%q,"error":"build exploded"}}}`, tc.id, tc.status)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			_, err := client.waitFunction(ctx, "p", "b", "api", 7, time.Millisecond)
			require.ErrorContains(t, err, tc.want)
		})
	}
	t.Run("in-flight HTTP cancellation", func(t *testing.T) {
		client, server := testBackendClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := client.getFunction(ctx, "p", "b", "api")
		require.Error(t, err)
		require.Less(t, time.Since(start), time.Second)
	})
}

func TestFunctionFailureRetainsTerraformIdentity(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "function.zip")
	writeTestArchive(t, filename, "export default { fetch: () => new Response('hi') }")
	m := &backendMock{t: t, fail: true}
	server := httptest.NewServer(m)
	defer server.Close()
	config := fmt.Sprintf(`provider "neon" { api_key = "test" }
resource "neon_function" "api" {
 project_id = "p"
 branch_id = "b"
 slug = "api"
 zip_file_path = %q
}`, filename)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: backendFactories(server), Steps: []resource.TestStep{
		{Config: config, ExpectError: regexp.MustCompile("synthetic build failure")},
		{PreConfig: func() { m.mu.Lock(); defer m.mu.Unlock(); m.fail = false }, Config: config, Check: resource.TestCheckResourceAttr("neon_function.api", "current_deployment_status", "completed")},
	}})
}

func TestBranchAuthRejectsDifferentDatabase(t *testing.T) {
	ctx := context.Background()
	m := &backendMock{t: t, auth: true, authDB: "someone_else"}
	client, server := testBackendClient(t, m)
	defer server.Close()
	r := &branchAuthResource{client: client}
	var schemaResp frameworkresource.SchemaResponse
	r.Schema(ctx, frameworkresource.SchemaRequest{}, &schemaResp)
	state := tfsdk.State{Schema: schemaResp.Schema}
	model := branchAuthModel{ID: types.StringValue("p/b/app"), ProjectID: types.StringValue("p"), BranchID: types.StringValue("b"), DatabaseName: types.StringValue("app")}
	require.False(t, state.Set(ctx, &model).HasError())
	readResp := frameworkresource.ReadResponse{State: state}
	r.Read(ctx, frameworkresource.ReadRequest{State: state}, &readResp)
	require.True(t, readResp.Diagnostics.HasError())
	var actual branchAuthModel
	require.False(t, readResp.State.Get(ctx, &actual).HasError())
	require.Equal(t, "p/b/app", actual.ID.ValueString())
	require.Equal(t, "app", actual.DatabaseName.ValueString())
	deleteResp := frameworkresource.DeleteResponse{State: state}
	r.Delete(ctx, frameworkresource.DeleteRequest{State: state}, &deleteResp)
	require.True(t, deleteResp.Diagnostics.HasError())
	require.Nil(t, m.deleteData)
	for _, id := range []string{"p/b/wrong", "/b/app", "p//app", "p/b/", "p/b/app/extra"} {
		resp := frameworkresource.ImportStateResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
		r.ImportState(ctx, frameworkresource.ImportStateRequest{ID: id}, &resp)
		require.True(t, resp.Diagnostics.HasError(), id)
	}
}

func TestBackendReadAndDeleteErrorsPreserveState(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client, server := testBackendClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"code":"ERROR","message":"refused"}`)
			}))
			defer server.Close()
			ctx := context.Background()
			for _, tc := range []struct {
				r     frameworkresource.Resource
				model any
			}{
				{&functionResource{client: client}, &functionModel{ID: types.StringValue("p/b/api"), ProjectID: types.StringValue("p"), BranchID: types.StringValue("b"), Slug: types.StringValue("api"), EnvironmentVariables: types.MapNull(types.StringType), EnvironmentVariableNames: types.SetNull(types.StringType)}},
				{&customBranchDomainResource{client: client}, &customBranchDomainModel{ID: types.StringValue("p/b/api.example.com"), ProjectID: types.StringValue("p"), BranchID: types.StringValue("b"), Domain: types.StringValue("api.example.com")}},
				{&branchAuthResource{client: client}, &branchAuthModel{ID: types.StringValue("p/b/app"), ProjectID: types.StringValue("p"), BranchID: types.StringValue("b"), DatabaseName: types.StringValue("app")}},
			} {
				var schemaResp frameworkresource.SchemaResponse
				tc.r.Schema(ctx, frameworkresource.SchemaRequest{}, &schemaResp)
				state := tfsdk.State{Schema: schemaResp.Schema}
				require.False(t, state.Set(ctx, tc.model).HasError())
				read := frameworkresource.ReadResponse{State: state}
				tc.r.Read(ctx, frameworkresource.ReadRequest{State: state}, &read)
				if status == http.StatusNotFound {
					require.False(t, read.Diagnostics.HasError())
					require.True(t, read.State.Raw.IsNull())
				} else {
					require.True(t, read.Diagnostics.HasError())
					require.True(t, state.Raw.Equal(read.State.Raw))
				}
				del := frameworkresource.DeleteResponse{State: state}
				tc.r.Delete(ctx, frameworkresource.DeleteRequest{State: state}, &del)
				if status == http.StatusNotFound {
					require.False(t, del.Diagnostics.HasError())
					require.True(t, del.State.Raw.IsNull())
				} else {
					require.True(t, del.Diagnostics.HasError())
					require.True(t, state.Raw.Equal(del.State.Raw))
				}
			}
		})
	}
}

func TestFunctionArchiveHashGuardsAndEnvironmentPatch(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "function.zip")
	writeTestArchive(t, filename, "export default {}")
	_, hash, err := readFunctionArchive(filename)
	require.NoError(t, err)
	writeTestArchive(t, filename, "export default { fetch() {} }")
	r := &functionResource{}
	_, accepted, err := r.deploy(context.Background(), &functionModel{ZipFilePath: types.StringValue(filename), SourceCodeHash: types.StringValue(hash)}, true)
	require.ErrorContains(t, err, "changed after planning")
	require.False(t, accepted)
	require.NoError(t, os.WriteFile(filename, []byte("not a zip"), 0600))
	_, _, err = readFunctionArchive(filename)
	require.ErrorContains(t, err, "invalid function ZIP")
	env, diags := types.MapValueFrom(context.Background(), types.StringType, map[string]string{"KEEP": "value"})
	require.False(t, diags.HasError())
	patch, err := functionEnvironment(context.Background(), env, []string{"KEEP", "REMOVE"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"KEEP": "value", "REMOVE": ""}, patch)
	patch, err = functionEnvironment(context.Background(), types.MapNull(types.StringType), []string{"KEEP"})
	require.NoError(t, err)
	require.Nil(t, patch)
}

func TestFunctionCreatedAtChanged(t *testing.T) {
	original := types.StringValue("2026-10-15T01:00:00.963066Z")
	for _, tc := range []struct {
		name     string
		previous types.String
		current  types.String
		changed  bool
	}{
		{"same value", original, original, false},
		{"equivalent precision and offset", original, types.StringValue("2026-10-15T03:00:00.963066000+02:00"), false},
		{"new generation", original, types.StringValue("2026-10-15T01:00:00.963067Z"), true},
		{"import without prior timestamp", types.StringNull(), original, false},
		{"partial create without prior timestamp", types.StringValue(""), original, false},
		{"unknown prior timestamp", types.StringUnknown(), original, false},
		{"changed unparseable timestamp", original, types.StringValue("unrecognized timestamp"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.changed, functionCreatedAtChanged(tc.previous, tc.current))
		})
	}
}
