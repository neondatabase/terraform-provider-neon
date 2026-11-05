package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestAcceptanceCleanupOnlyDeletesOwnedProjects(t *testing.T) {
	t.Parallel()
	api := &cleanupHTTP{pages: map[string]cleanupPage{}}
	projects, _ := cleanupFixture(t, api)
	name := projects.NewName("pr-")
	api.pages[name+"|"] = cleanupPage{projects: []neon.Project{
		{ID: "p-unrelated", Name: "pr-production"},
		{ID: "p-suffix", Name: name + "-copy"},
		{ID: "p-owned", Name: name},
	}}
	require.NoError(t, projects.cleanup(context.Background()))
	require.Equal(t, []string{"p-owned"}, api.deleted())
	require.Equal(t, []string{name + "|"}, api.listed())
}

func TestAcceptanceCleanupSavedIDsSurviveRenameAndReplacement(t *testing.T) {
	t.Parallel()
	api := &cleanupHTTP{}
	projects, _ := cleanupFixture(t, api)
	before, after := projects.NewName("pr-before-"), projects.NewName("pr-after-")
	require.NoError(t, projects.RecordProject(neon.Project{ID: "p-old", Name: before}))
	require.NoError(t, projects.RecordProject(neon.Project{ID: "p-old", Name: after}))
	require.NoError(t, projects.RecordProject(neon.Project{ID: "p-new", Name: after}))
	require.NoError(t, projects.cleanup(context.Background()))
	require.Equal(t, []string{"p-new", "p-old"}, api.deleted())
	require.Empty(t, api.listed(), "captured names need no recovery lookup")
}

func TestAcceptanceCleanupRecoveryPagination(t *testing.T) {
	t.Parallel()
	api := &cleanupHTTP{pages: map[string]cleanupPage{}}
	projects, _ := cleanupFixture(t, api)
	name := projects.NewName("bucket")
	api.pages[name+"|"] = cleanupPage{projects: []neon.Project{{ID: "p-other", Name: "bucket"}}, cursor: "next"}
	api.pages[name+"|next"] = cleanupPage{projects: []neon.Project{{ID: "p-owned", Name: name}}}
	require.NoError(t, projects.cleanup(context.Background()))
	require.Equal(t, []string{"p-owned"}, api.deleted())
	require.Equal(t, []string{name + "|", name + "|next"}, api.listed())
}

func TestAcceptanceCleanupFailsClosed(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"ambiguous exact name", "missing ID", "list failure", "repeated cursor", "later page failure"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			api := &cleanupHTTP{pages: map[string]cleanupPage{}}
			projects, _ := cleanupFixture(t, api)
			saved := projects.NewName("saved-")
			require.NoError(t, projects.RecordProject(neon.Project{ID: "p-saved", Name: saved}))
			name := projects.NewName("pr-")
			switch reason {
			case "ambiguous exact name":
				api.pages[name+"|"] = cleanupPage{projects: []neon.Project{{ID: "p-one", Name: name}, {ID: "p-two", Name: name}}}
			case "missing ID":
				api.pages[name+"|"] = cleanupPage{projects: []neon.Project{{Name: name}}}
			case "list failure":
				api.pages[name+"|"] = cleanupPage{status: 500}
			case "repeated cursor":
				api.pages[name+"|"] = cleanupPage{cursor: "repeat"}
				api.pages[name+"|repeat"] = cleanupPage{cursor: "repeat"}
			case "later page failure":
				api.pages[name+"|"] = cleanupPage{projects: []neon.Project{{ID: "p-owned", Name: name}}, cursor: "next"}
				api.pages[name+"|next"] = cleanupPage{status: 500}
			}
			require.Error(t, projects.cleanup(context.Background()))
			require.Empty(t, api.deleted(), "resolve and validate all targets before any mutation")
		})
	}
}

func TestAcceptanceCleanupDeleteErrors(t *testing.T) {
	t.Parallel()
	for _, status := range []int{404, 403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			api := &cleanupHTTP{deleteStatus: map[string]int{"p-first": status}}
			projects, _ := cleanupFixture(t, api)
			name := projects.NewName("pr-")
			require.NoError(t, projects.RecordProject(neon.Project{ID: "p-first", Name: name}))
			require.NoError(t, projects.RecordProject(neon.Project{ID: "p-second", Name: name}))
			err := projects.cleanup(context.Background())
			if status == 404 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), "p-first")
			}
			require.Equal(t, []string{"p-first", "p-second"}, api.deleted(), "one error must not hide another owned target")
		})
	}
}

func TestAcceptanceCleanupNoTargetsOrCancelledContext(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			t.Parallel()
			api := &cleanupHTTP{}
			projects, _ := cleanupFixture(t, api)
			ctx := context.Background()
			if cancel {
				projects.NewName("pr-")
				var stop context.CancelFunc
				ctx, stop = context.WithCancel(ctx)
				stop()
				require.ErrorIs(t, projects.cleanup(ctx), context.Canceled)
			} else {
				require.NoError(t, projects.cleanup(ctx))
			}
			require.Empty(t, api.deleted())
			require.Empty(t, api.listed())
		})
	}
}

func TestAcceptanceCleanupRejectsUnregisteredState(t *testing.T) {
	t.Parallel()
	for _, project := range []neon.Project{{ID: "p-other", Name: "pr-existing"}, {Name: ""}} {
		api := &cleanupHTTP{}
		projects, _ := cleanupFixture(t, api)
		require.Error(t, projects.RecordProject(project))
		require.NoError(t, projects.cleanup(context.Background()))
		require.Empty(t, api.deleted())
		require.Empty(t, api.listed())
	}
}

func TestAcceptanceCleanupCaseCapturesBeforeFailedCheck(t *testing.T) {
	t.Parallel()
	api := &cleanupHTTP{}
	projects, _ := cleanupFixture(t, api)
	name := projects.NewName("pr-")
	checkErr := errors.New("original check failed")
	preCalled := false
	original := resource.TestCase{Steps: []resource.TestStep{{
		PreConfig: func() { preCalled = true },
		Check:     func(*terraform.State) error { return checkErr },
	}}}
	wrapped := projects.prepare(original)
	wrapped.Steps[0].PreConfig()
	require.True(t, preCalled)
	require.ErrorIs(t, wrapped.Steps[0].Check(cleanupState("p-owned", name)), checkErr)
	require.NoError(t, projects.cleanup(context.Background()))
	require.Equal(t, []string{"p-owned"}, api.deleted())
	require.Empty(t, api.listed())
	require.ErrorIs(t, original.Steps[0].Check(cleanupState("p-foreign", "unregistered")), checkErr,
		"preparation must not mutate reusable test cases")
}

func TestAcceptanceCleanupFailedApplyRecoversReplacement(t *testing.T) {
	t.Parallel()
	api := &cleanupHTTP{pages: map[string]cleanupPage{}}
	projects, _ := cleanupFixture(t, api)
	name := projects.NewName("pr-")
	require.NoError(t, projects.RecordProject(neon.Project{ID: "p-old", Name: name}))
	wrapped := projects.prepare(resource.TestCase{Steps: []resource.TestStep{{}}})
	require.NotNil(t, wrapped.Steps[0].PreConfig)
	wrapped.Steps[0].PreConfig()
	// The apply fails before Check runs; recover only the exact registered name.
	api.pages[name+"|"] = cleanupPage{projects: []neon.Project{{ID: "p-new", Name: name}, {ID: "p-production", Name: "pr-production"}}}
	require.NoError(t, projects.cleanup(context.Background()))
	require.Equal(t, []string{"p-new", "p-old"}, api.deleted())
}

func TestAcceptanceCleanupConcurrentOwners(t *testing.T) {
	t.Parallel()
	api := &cleanupHTTP{}
	first, _ := cleanupFixture(t, api)
	second, _ := cleanupFixture(t, api)
	var wg sync.WaitGroup
	for _, tt := range []struct {
		owner *acceptanceProjects
		id    string
	}{{first, "p-one"}, {second, "p-two"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := tt.owner.NewName("same-prefix-")
			if err := tt.owner.RecordProject(neon.Project{ID: tt.id, Name: name}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	require.NoError(t, first.cleanup(context.Background()))
	require.Equal(t, []string{"p-one"}, api.deleted(), "first owner must not consume second owner's ID")
	require.NoError(t, second.cleanup(context.Background()))
	require.Equal(t, []string{"p-one", "p-two"}, api.deleted())
}

func TestAcceptanceCleanupConcurrentRegistrations(t *testing.T) {
	t.Parallel()
	api := &cleanupHTTP{}
	projects, _ := cleanupFixture(t, api)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := projects.NewName("concurrent-")
			if err := projects.RecordProject(neon.Project{ID: fmt.Sprintf("p-%02d", i), Name: name}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	require.NoError(t, projects.cleanup(context.Background()))
	require.Len(t, api.deleted(), 16)
	require.Empty(t, api.listed())
}

func TestAcceptanceCleanupReportsRegisteredErrors(t *testing.T) {
	t.Parallel()
	api := &cleanupHTTP{deleteStatus: map[string]int{"p-owned": 500}}
	projects, reporter := cleanupFixture(t, api)
	name := projects.NewName("pr-")
	require.NoError(t, projects.RecordProject(neon.Project{ID: "p-owned", Name: name}))
	require.Len(t, reporter.callbacks, 1)
	reporter.callbacks[0]()
	require.Len(t, reporter.failures, 1)
	require.Contains(t, reporter.failures[0], "p-owned")
}

func cleanupState(id, name string) *terraform.State {
	return &terraform.State{Modules: []*terraform.ModuleState{{Path: []string{"root"}, Resources: map[string]*terraform.ResourceState{
		"neon_project.this": {Type: "neon_project", Primary: &terraform.InstanceState{ID: id, Attributes: map[string]string{"name": name}}},
	}}}}
}

func cleanupFixture(t *testing.T, api *cleanupHTTP) (*acceptanceProjects, *cleanupReporter) {
	t.Helper()
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	reporter := &cleanupReporter{}
	return newAcceptanceProjects(reporter, client), reporter
}

type cleanupReporter struct {
	callbacks []func()
	failures  []string
}

func (r *cleanupReporter) Cleanup(callback func()) { r.callbacks = append(r.callbacks, callback) }
func (r *cleanupReporter) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

type cleanupPage struct {
	projects []neon.Project
	cursor   string
	status   int
}

// Real SDK query serialization and error conversion remain active. This
// transport refuses any method other than scoped list and project delete.
type cleanupHTTP struct {
	mu           sync.Mutex
	pages        map[string]cleanupPage
	deleteStatus map[string]int
	listQueries  []string
	deletes      []string
}

func (f *cleanupHTTP) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	status, body := 200, `{"project":{}}`
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/api/v2/projects":
		key := req.URL.Query().Get("search") + "|" + req.URL.Query().Get("cursor")
		if req.URL.Query().Get("search") == "" {
			return nil, errors.New("unscoped project listing refused")
		}
		f.listQueries = append(f.listQueries, key)
		page := f.pages[key]
		if page.status != 0 {
			status = page.status
		}
		payload := map[string]any{"projects": page.projects}
		if page.cursor != "" {
			payload["pagination"] = map[string]string{"cursor": page.cursor}
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = string(data)
	case req.Method == http.MethodDelete && strings.HasPrefix(req.URL.Path, "/api/v2/projects/"):
		id := strings.TrimPrefix(req.URL.Path, "/api/v2/projects/")
		if strings.Contains(id, "/") || id == "" {
			return nil, fmt.Errorf("invalid delete target %q", id)
		}
		f.deletes = append(f.deletes, id)
		if f.deleteStatus[id] != 0 {
			status = f.deleteStatus[id]
		}
	default:
		return nil, fmt.Errorf("unregistered HTTP operation: %s %s", req.Method, req.URL.Path)
	}
	if status >= 400 {
		body = `{"message":"fixture error"}`
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (f *cleanupHTTP) deleted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := slices.Clone(f.deletes)
	slices.Sort(result)
	return result
}

func (f *cleanupHTTP) listed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.listQueries)
}
