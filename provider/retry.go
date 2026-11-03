package provider

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	neon "github.com/kislerdm/neon-sdk-go"
)

type delay struct {
	delay  time.Duration
	maxCnt uint8
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
			switch e.HTTPCode {
			case 200:
				return nil
			case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusLocked,
				http.StatusServiceUnavailable:
				tflog.Debug(ctx, "API call delay "+strconv.FormatInt(r.delay.Milliseconds(), 10)+" ms.")
				err = e
				i++
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(r.delay):
				}
			default:
				if fallback, ok := fallbacks[e.HTTPCode]; ok {
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

// TODO: refactor ctx to be the arg 0.
func (r *delay) RetryFramework(fn func(context.Context) error, ctx context.Context) diag.Diagnostics {
	return frameworkDiagnostics(r.Do(ctx, fn, nil))
}

// TODO: refactor ctx to be the arg 0.
func (r *delay) RetryWithFallbackFramework(fn func(context.Context) error, ctx context.Context,
	fallbacks map[int]func(context.Context) error) diag.Diagnostics {
	return frameworkDiagnostics(r.Do(ctx, fn, fallbacks))
}

func frameworkDiagnostics(err error) diag.Diagnostics {
	if err == nil {
		return nil
	}

	var diags diag.Diagnostics
	diags.AddError("Neon API request failed", err.Error())
	return diags
}

var projectReadiness = delay{
	delay:  1 * time.Second,
	maxCnt: 120,
}
