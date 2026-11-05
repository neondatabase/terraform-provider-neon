package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

type acceptanceCleanupReporter interface {
	Cleanup(func())
	Errorf(string, ...any)
}

// Ownership stays local to one test. Names recover IDs after failed applies;
// captured IDs retain ownership across renames and replacements.
type acceptanceProjects struct {
	client *neon.Client
	mu     sync.Mutex
	names  map[string]bool
	ids    map[string]struct{}
}

func newAcceptanceProjects(t acceptanceCleanupReporter, client *neon.Client) *acceptanceProjects {
	projects := &acceptanceProjects{
		client: client,
		names:  make(map[string]bool),
		ids:    make(map[string]struct{}),
	}
	t.Cleanup(func() {
		if err := projects.cleanup(context.Background()); err != nil {
			t.Errorf("project cleanup: %v", err)
		}
	})
	return projects
}

// NewName registers a UUID name before creation so failed applies can recover it.
func (p *acceptanceProjects) NewName(prefix string) string {
	name := newProjectName(prefix)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.names[name] = true
	return name
}

// RecordProject keeps IDs across rename/replacement without trusting a prefix.
func (p *acceptanceProjects) RecordProject(project neon.Project) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, registered := p.names[project.Name]; !registered || project.ID == "" {
		return fmt.Errorf("project %q (%q) is not a registered test-owned project", project.Name, project.ID)
	}
	p.ids[project.ID] = struct{}{}
	p.names[project.Name] = false
	return nil
}

func (p *acceptanceProjects) captureState(state *terraform.State) error {
	if state == nil {
		return nil
	}
	for _, module := range state.Modules {
		if module == nil {
			continue
		}
		for _, r := range module.Resources {
			if r == nil || r.Type != "neon_project" || r.Primary == nil || r.Primary.ID == "" {
				continue
			}
			if err := p.RecordProject(neon.Project{ID: r.Primary.ID, Name: r.Primary.Attributes["name"]}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *acceptanceProjects) prepare(test resource.TestCase) resource.TestCase {
	// Cases and steps are often reused. Do not accumulate wrapper closures.
	test.Steps = slices.Clone(test.Steps)
	for i := range test.Steps {
		preConfig, check := test.Steps[i].PreConfig, test.Steps[i].Check
		test.Steps[i].PreConfig = func() {
			p.mu.Lock()
			for name := range p.names {
				p.names[name] = true
			}
			p.mu.Unlock()
			if preConfig != nil {
				preConfig()
			}
		}
		test.Steps[i].Check = func(state *terraform.State) error {
			// Capture before another check can fail and abort the test case.
			if err := p.captureState(state); err != nil {
				return err
			}
			if check != nil {
				return check(state)
			}
			return nil
		}
	}
	return test
}

// Test captures ownership while retaining the original acceptance runner's gates.
func (p *acceptanceProjects) Test(t *testing.T, test resource.TestCase) {
	t.Helper()
	resource.Test(t, p.prepare(test))
}

func (p *acceptanceProjects) cleanup(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	names, ids := maps.Clone(p.names), maps.Clone(p.ids)
	p.mu.Unlock()

	var resolutionErrors []error
	for _, name := range slices.Sorted(maps.Keys(names)) {
		if !names[name] {
			continue
		}
		matches, err := p.findExactProjects(ctx, name)
		if err != nil {
			resolutionErrors = append(resolutionErrors, err)
			continue
		}
		if len(matches) > 1 {
			resolutionErrors = append(resolutionErrors, fmt.Errorf("ambiguous cleanup name %q: found %d project IDs", name, len(matches)))
			continue
		}
		for id := range matches {
			ids[id] = struct{}{}
		}
	}
	// Resolve every fallback before mutating, so a failed lookup cannot cause
	// a partially guessed cleanup. Saved IDs are never inferred from prefixes.
	if err := errors.Join(resolutionErrors...); err != nil {
		return err
	}

	var deleteErrors []error
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(deleteErrors, err)...)
		}
		if _, err := p.client.DeleteProject(id); err != nil && !cleanupProjectNotFound(err) {
			deleteErrors = append(deleteErrors, fmt.Errorf("delete test project %s: %w", id, err))
		}
	}
	return errors.Join(deleteErrors...)
}

func (p *acceptanceProjects) findExactProjects(ctx context.Context, name string) (map[string]struct{}, error) {
	ids := make(map[string]struct{})
	var cursor *string
	seen := make(map[string]struct{})
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response, err := p.client.ListProjects(cursor, nil, &name, nil, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("find exact test project %q: %w", name, err)
		}
		for _, project := range response.Projects {
			if project.Name != name {
				continue
			}
			if project.ID == "" {
				return nil, fmt.Errorf("find exact test project %q: response has no ID", name)
			}
			ids[project.ID] = struct{}{}
		}
		if response.Pagination == nil || response.Pagination.Cursor == "" {
			return ids, nil
		}
		next := response.Pagination.Cursor
		if _, repeated := seen[next]; repeated {
			return nil, fmt.Errorf("find exact test project %q: repeated cursor %q", name, next)
		}
		seen[next] = struct{}{}
		cursor = &next
	}
}

func cleanupProjectNotFound(err error) bool {
	var value neon.Error
	var pointer *neon.Error
	switch {
	case errors.As(err, &value):
		return value.HTTPCode == http.StatusNotFound
	case errors.As(err, &pointer) && pointer != nil:
		return pointer.HTTPCode == http.StatusNotFound
	default:
		return false
	}
}
