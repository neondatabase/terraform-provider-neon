package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	neon "github.com/kislerdm/neon-sdk-go"
)

type clientFactory func(context.Context) (*neon.Client, error)

type cliBoundHTTP struct {
	ctx  context.Context
	base interface {
		Do(*http.Request) (*http.Response, error)
	}
}

func (c *cliBoundHTTP) Do(req *http.Request) (*http.Response, error) {
	return c.base.Do(req.Clone(c.ctx))
}

type projectIDs []string

// String formats IDs for flag help without changing their meaning.
func (v *projectIDs) String() string { return strings.Join(*v, ",") }

// Set accepts one opaque ID rather than expanding user-supplied selectors.
func (v *projectIDs) Set(id string) error {
	// IDs are opaque. Refuse paths, URLs, lists and wildcard expansion rather
	// than deriving targets from them or searching an account.
	if !projectIDPattern.MatchString(id) {
		return fmt.Errorf("invalid project ID %q; supply one ID per --project-id flag", id)
	}
	*v = append(*v, id)
	return nil
}

var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

type cleanupTarget struct {
	id        string
	name      string
	protected bool
}

func run(ctx context.Context, args []string, out io.Writer, factory clientFactory) error {
	flags := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	flags.SetOutput(out)
	var ids projectIDs
	var execute bool
	var timeout time.Duration
	flags.Var(&ids, "project-id", "Explicit project ID; repeat for each target (required)")
	flags.BoolVar(&execute, "execute", false, "Delete selected projects after preflight; default is read-only preview")
	flags.DurationVar(&timeout, "timeout", 2*time.Minute, "Total time limit, including preflight and deletion")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments; supply explicit --project-id flags")
	}
	if len(ids) == 0 {
		return fmt.Errorf("at least one --project-id is required; account-wide cleanup is not supported")
	}
	if timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := factory(ctx)
	if err != nil {
		return fmt.Errorf("configure cleanup client: %w", err)
	}

	seen := make(map[string]struct{})
	var targets []cleanupTarget
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		target, err := preflightTarget(ctx, client, id)
		if err != nil {
			return err
		}
		targets = append(targets, target)
	}

	mode := "preview (read-only; --execute is required for deletion)"
	if execute {
		mode = "execute"
	}
	if _, err := fmt.Fprintln(out, "mode:", mode); err != nil {
		return fmt.Errorf("write cleanup preview: %w", err)
	}
	for _, target := range targets {
		if _, err := fmt.Fprintf(out, "%s\t%q\tprotected=%t\n", target.id, target.name, target.protected); err != nil {
			return fmt.Errorf("write cleanup target %s: %w", target.id, err)
		}
	}
	if !execute {
		return nil
	}
	// Validate protection for every target before the first deletion. This
	// command deliberately has no capability to unprotect branches.
	for _, target := range targets {
		if target.protected {
			return fmt.Errorf("refusing execution: project %s has protected branches; no projects deleted", target.id)
		}
	}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := client.DeleteProject(target.id); err != nil {
			return fmt.Errorf("delete project %s: %w", target.id, err)
		}
		if _, err := fmt.Fprintln(out, "deleted", target.id); err != nil {
			return fmt.Errorf("report deletion of project %s: %w", target.id, err)
		}
	}
	return nil
}

func preflightTarget(ctx context.Context, client *neon.Client, id string) (cleanupTarget, error) {
	if err := ctx.Err(); err != nil {
		return cleanupTarget{}, err
	}
	response, err := client.GetProject(id)
	if err != nil {
		return cleanupTarget{}, fmt.Errorf("read project %s: %w", id, err)
	}
	if response.Project.ID != id {
		return cleanupTarget{}, fmt.Errorf("read project %s: response ID %q does not match explicit target", id, response.Project.ID)
	}
	target := cleanupTarget{id: id, name: response.Project.Name}
	var cursor *string
	seen := make(map[string]struct{})
	for {
		if err := ctx.Err(); err != nil {
			return cleanupTarget{}, err
		}
		branches, err := client.ListProjectBranches(id, nil, nil, cursor, nil, nil, nil)
		if err != nil {
			return cleanupTarget{}, fmt.Errorf("list branches for project %s: %w", id, err)
		}
		if branches.Branches == nil {
			return cleanupTarget{}, fmt.Errorf("list branches for project %s: missing branch list", id)
		}
		for _, branch := range branches.Branches {
			if branch.ProjectID != id || branch.ID == "" {
				return cleanupTarget{}, fmt.Errorf("list branches for project %s: invalid branch identity %q (%q)", id, branch.ID, branch.ProjectID)
			}
			target.protected = target.protected || branch.Protected
		}
		if branches.Pagination == nil || branches.Pagination.Next == nil || *branches.Pagination.Next == "" {
			return target, nil
		}
		next := *branches.Pagination.Next
		if _, repeated := seen[next]; repeated {
			return cleanupTarget{}, fmt.Errorf("list branches for project %s: repeated cursor %q", id, next)
		}
		seen[next] = struct{}{}
		cursor = &next
	}
}
