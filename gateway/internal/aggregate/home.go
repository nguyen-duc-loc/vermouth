package aggregate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	urlpkg "net/url"

	"golang.org/x/sync/errgroup"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/apitypes"
)

var errUpstreamStatus = errors.New("upstream answered with an unexpected status")

const billingUnavailableMessage = "billing projection is temporarily unavailable"

// RequiredResponseError carries a required service boundary answer back to the
// route, so declared 4xx errors keep their status and API error shape.
type RequiredResponseError struct {
	Service  string
	Response Response
}

func (e *RequiredResponseError) Error() string {
	return fmt.Sprintf("%s answered %d: %v", e.Service, e.Response.Status, errUpstreamStatus)
}

// Home fans out to required identity and teaching reads plus optional billing
// progress. A billing failure is represented only in its panel.
func (c *Client) Home(ctx context.Context, bearer, cursor string) (apitypes.Home, error) {
	status := apitypes.Home{Sessions: []apitypes.HomeSession{}}
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		response, err := c.Call(groupCtx, http.MethodGet, c.upstreams.Identity, "/me", bearer, nil)
		if err != nil {
			return fmt.Errorf("read identity for home: %w", err)
		}
		if response.Status != http.StatusOK {
			return &RequiredResponseError{Service: "identity", Response: response}
		}
		err = json.Unmarshal(response.Body, &status.Tutor)
		if err != nil {
			return fmt.Errorf("decode identity home answer: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		path := "/home"
		if cursor != "" {
			path += "?cursor=" + urlpkg.QueryEscape(cursor)
		}
		response, err := c.Call(groupCtx, http.MethodGet, c.upstreams.Teaching, path, bearer, nil)
		if err != nil {
			return fmt.Errorf("read teaching home: %w", err)
		}
		if response.Status != http.StatusOK {
			return &RequiredResponseError{Service: "teaching", Response: response}
		}
		var teaching apitypes.TeachingHome
		err = json.Unmarshal(response.Body, &teaching)
		if err != nil {
			return fmt.Errorf("decode teaching home answer: %w", err)
		}
		status.LocalDate = teaching.LocalDate
		status.NextCursor = teaching.NextCursor
		status.NextLocalMidnightAt = teaching.NextLocalMidnightAt
		status.RequestTimeZone = teaching.RequestTimeZone
		status.Sessions = teaching.Sessions
		status.SetupDefaults = teaching.SetupDefaults
		return nil
	})
	var billingErr error
	group.Go(func() error {
		status.BillingProjection, billingErr = c.readBillingProjection(groupCtx, bearer)
		return nil
	})
	err := group.Wait()
	if err != nil {
		return apitypes.Home{}, err
	}
	if billingErr != nil {
		message := billingUnavailableMessage
		status.BillingProjectionUnavailable = &message
		status.BillingProjection = nil
	}
	return status, nil
}

// HomeBillingProjection refreshes only billing, so polling never reloads
// identity or teaching and never blocks attendance.
func (c *Client) HomeBillingProjection(
	ctx context.Context,
	bearer string,
) apitypes.HomeBillingProjection {
	projection, err := c.readBillingProjection(ctx, bearer)
	if err != nil {
		message := billingUnavailableMessage
		return apitypes.HomeBillingProjection{
			BillingProjection:            nil,
			BillingProjectionUnavailable: &message,
		}
	}
	return apitypes.HomeBillingProjection{
		BillingProjection:            projection,
		BillingProjectionUnavailable: nil,
	}
}

func (c *Client) readBillingProjection(
	ctx context.Context,
	bearer string,
) (*apitypes.BillingProjection, error) {
	response, err := c.Call(
		ctx,
		http.MethodGet,
		c.upstreams.Billing,
		"/projections/teaching/status",
		bearer,
		nil,
	)
	if err != nil {
		return nil, err
	}
	if response.Status != http.StatusOK {
		return nil, fmt.Errorf("billing answered %d: %w", response.Status, errUpstreamStatus)
	}
	var projection apitypes.BillingProjection
	err = json.Unmarshal(response.Body, &projection)
	if err != nil {
		return nil, fmt.Errorf("decode billing projection answer: %w", err)
	}
	return &projection, nil
}
