package provider

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	neon "github.com/kislerdm/neon-sdk-go"
)

type delay struct {
	delay   time.Duration
	maxCnt  uint8
	retryOn []int
}

func (r *delay) Do(ctx context.Context, fn func(context.Context) error,
	fallbacks map[int]func(context.Context) error) error {
	var i uint8
	var err error
	for i < r.maxCnt {
		tflog.Debug(ctx, "API call attempt "+strconv.Itoa(int(i)))

		switch e := fn(ctx).(type) {
		case nil:
			return nil
		case neon.Error:
			tflog.Debug(ctx, "API call error code: "+strconv.Itoa(e.HTTPCode))
			switch {
			case e.HTTPCode == 200:
				return nil
			case slices.Contains(r.retryOn, e.HTTPCode):
				tflog.Debug(ctx, "API call delay "+strconv.FormatInt(r.delay.Milliseconds(), 10)+" ms.")
				err = e
				i++
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(r.delay):
				}
			default:
				if fallback, ok := fallbacks[e.HTTPCode]; ok && !requiresPersonalAPIKey(e) {
					tflog.Debug(ctx, "API call fallback for error code: "+strconv.Itoa(e.HTTPCode))
					return r.Do(ctx, fallback, nil)
				}
				return e
			}
		default:
			return e
		}
	}
	return err
}

func (r *delay) Retry(ctx context.Context, fn func(context.Context) error,
	fallbacks map[int]func(context.Context) error) diag.Diagnostics {
	return newDiagnostics(r.Do(ctx, fn, fallbacks))
}

var projectReadiness = delay{
	delay:  1 * time.Second,
	maxCnt: 120,
	retryOn: []int{
		http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusLocked, http.StatusServiceUnavailable,
	},
}

// A 5xx answer to a create request can arrive after Neon created the resource, so repeating the request could
// create a second resource that Terraform never records. Create requests repeat only on answers that mean Neon
// refused the request.
var creation = delay{
	delay:   projectReadiness.delay,
	maxCnt:  projectReadiness.maxCnt,
	retryOn: []int{http.StatusTooManyRequests, http.StatusLocked},
}

// retryCreate sends a request that creates a Neon resource.
func retryCreate(ctx context.Context, fn func(context.Context) error) diag.Diagnostics {
	err := creation.Do(ctx, fn, nil)
	var apiErr neon.Error
	if !errors.As(err, &apiErr) || apiErr.HTTPCode < http.StatusInternalServerError {
		return newDiagnostics(err)
	}
	var diags diag.Diagnostics
	diags.AddError("Neon may have created the resource", err.Error()+"\n\nThe create request failed with a server error, "+
		"which Neon can return after creating the resource. Check the Neon Console for it before running Terraform "+
		"again. If it exists, import it with terraform import or delete it, otherwise the next apply creates a second one.")
	return diags
}
