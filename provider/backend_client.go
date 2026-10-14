package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	neon "github.com/kislerdm/neon-sdk-go"
)

const neonAPIURL = "https://console.neon.tech/api/v2"

// The generated SDK has no context parameter and leaves error response bodies
// open. Scope its transport to one operation, without mutating the shared client.
type scopedHTTPClient struct {
	ctx    context.Context
	client neon.HTTPClient
	bodies []io.ReadCloser
}

func (c *scopedHTTPClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.client.Do(req.Clone(c.ctx))
	if resp != nil && resp.Body != nil {
		c.bodies = append(c.bodies, resp.Body)
	}
	return resp, err
}

func (c *neonClient) scopedSDK(ctx context.Context) (*neon.Client, func(), error) {
	httpClient := c.sdkCfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	transport := &scopedHTTPClient{ctx: ctx, client: httpClient}
	cfg := c.sdkCfg
	cfg.HTTPClient = transport
	client, err := neon.NewClient(cfg)
	return client, func() {
		for _, body := range transport.bodies {
			_ = body.Close()
		}
	}, err
}

func apiNotFound(err error) bool {
	var apiErr neon.Error
	return errors.As(err, &apiErr) && apiErr.HTTPCode == http.StatusNotFound
}

func splitBackendID(id string) ([]string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, fmt.Errorf("expected <project_id>/<branch_id>/<resource_name>, got %q", id)
	}
	return parts, nil
}

func (c *neonClient) getFunction(ctx context.Context, projectID, branchID, slug string) (neon.NeonFunction, error) {
	client, closeBodies, err := c.scopedSDK(ctx)
	defer closeBodies()
	if err != nil {
		return neon.NeonFunction{}, err
	}
	resp, err := client.GetProjectBranchFunction(url.PathEscape(projectID), url.PathEscape(branchID), url.PathEscape(slug))
	if err == nil && (resp.Function.ID == "" || resp.Function.Slug != slug) {
		err = fmt.Errorf("invalid function response for slug %q", slug)
	}
	return resp.Function, err
}

// The SDK omits environment when len(map)==0. Send even an explicit {} and
// bind upload/response handling to Terraform's cancellable operation context.
func (c *neonClient) deployFunction(ctx context.Context, projectID, branchID, slug string,
	archive []byte, environment map[string]string, runtime string) (neon.NeonFunctionDeployment, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if archive != nil {
		part, err := w.CreateFormFile("zip", "function.zip")
		if err != nil {
			return neon.NeonFunctionDeployment{}, err
		}
		if _, err := part.Write(archive); err != nil {
			return neon.NeonFunctionDeployment{}, err
		}
	}
	if environment != nil {
		env, err := json.Marshal(environment)
		if err != nil {
			return neon.NeonFunctionDeployment{}, err
		}
		if err := w.WriteField("environment", string(env)); err != nil {
			return neon.NeonFunctionDeployment{}, err
		}
	}
	if err := w.WriteField("runtime", runtime); err != nil {
		return neon.NeonFunctionDeployment{}, err
	}
	if err := w.Close(); err != nil {
		return neon.NeonFunctionDeployment{}, err
	}
	endpoint := neonAPIURL + "/projects/" + url.PathEscape(projectID) + "/branches/" +
		url.PathEscape(branchID) + "/functions/" + url.PathEscape(slug) + "/deployments"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return neon.NeonFunctionDeployment{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.sdkCfg.Key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", w.FormDataContentType())
	httpClient := c.sdkCfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return neon.NeonFunctionDeployment{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Avoid echoing a body that might contain submitted environment secrets.
		return neon.NeonFunctionDeployment{}, fmt.Errorf("neon deployment returned HTTP %d (request ID %s)",
			resp.StatusCode, resp.Header.Get("X-Request-ID"))
	}
	var result neon.NeonFunctionDeploymentResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return neon.NeonFunctionDeployment{}, fmt.Errorf("decode deployment response: %w", err)
	}
	if result.Deployment.ID <= 0 {
		return neon.NeonFunctionDeployment{}, errors.New("deployment response has no valid deployment ID")
	}
	return result.Deployment, nil
}

func (c *neonClient) waitFunction(ctx context.Context, projectID, branchID, slug string,
	id int32, interval time.Duration) (neon.NeonFunction, error) {
	var latest neon.NeonFunction
	for {
		fn, err := c.getFunction(ctx, projectID, branchID, slug)
		if err != nil && !apiNotFound(err) {
			return latest, err
		}
		if err == nil {
			latest = fn
			if current := fn.CurrentDeployment; current != nil {
				if current.ID > id {
					return latest, fmt.Errorf("deployment %d was superseded by deployment %d; retry after resolving concurrent deployments", id, current.ID)
				}
				if current.ID == id {
					if current.Status == neon.NeonFunctionDeploymentStatusFailed {
						reason := "no build error supplied by Neon"
						if current.Error != nil {
							reason = *current.Error
						}
						return latest, fmt.Errorf("deployment %d failed: %s", id, reason)
					}
					if current.Status == neon.NeonFunctionDeploymentStatusCompleted && fn.ActiveDeployment != nil &&
						fn.ActiveDeployment.ID == id && fn.ActiveDeployment.Status == neon.NeonFunctionDeploymentStatusCompleted {
						return latest, nil
					}
				}
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return latest, fmt.Errorf("waiting for deployment %d: %w", id, ctx.Err())
		case <-timer.C:
		}
	}
}
