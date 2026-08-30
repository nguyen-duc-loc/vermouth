// Package aggregate reads from services on the gateway's behalf, including the
// read only fan out that lets one phone first screen ask two services at once
// (spec 0001, gateway read aggregation).
package aggregate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"time"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
)

// maxUpstreamBody caps how much of a service's answer the gateway will read,
// so one oversized body cannot take the gateway's memory with it.
const maxUpstreamBody = 4 << 20

// upstreamTimeout is shared by every identity, teaching, and billing call. It
// is code rather than deployment state because this feature adds no setting.
const upstreamTimeout = 5 * time.Second

// Upstreams are the services the gateway may call. It is the only caller
// allowed (INV-1), and it holds no database of its own.
type Upstreams struct {
	Identity      string
	Teaching      string
	Billing       string
	Notifications string
}

// Client calls services. Every call carries the request id, and the token
// unchanged when there is one (INV-15).
type Client struct {
	http      *http.Client
	upstreams Upstreams
}

// NewClient builds the one bounded client the gateway uses for every service.
func NewClient(upstreams Upstreams) *Client {
	return &Client{
		http: &http.Client{
			Timeout: upstreamTimeout,
			// A redirect is an answer here, not something to chase: identity's
			// sign in endpoints answer 302 and the browser is the one that has
			// to see it. No other call expects one at all.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		upstreams: upstreams,
	}
}

// Upstreams reports the configured service addresses, so routes and the
// readiness check use the same ones.
func (c *Client) Upstreams() Upstreams { return c.upstreams }

// Response is one service's answer, kept raw so a service's error body, which
// is already in the one error shape, can be passed through unchanged. The
// headers come with it, because sign in answers with two the browser needs.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Call makes one request to one service.
func (c *Client) Call(ctx context.Context, method, baseURL, path, bearer string, body any) (Response, error) {
	return c.CallWithHeaders(ctx, method, baseURL, path, bearer, nil, body)
}

// CallWithHeaders makes one service request with the narrow extra headers a
// public command contract requires, such as Idempotency-Key.
func (c *Client) CallWithHeaders(
	ctx context.Context,
	method string,
	baseURL string,
	path string,
	bearer string,
	headers http.Header,
	body any,
) (Response, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return Response{}, fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, baseURL+path, reader)
	if err != nil {
		return Response{}, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if requestID := vermouth.RequestID(ctx); requestID != "" {
		request.Header.Set(vermouth.RequestIDHeader, requestID)
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}

	response, err := c.http.Do(request)
	if err != nil {
		return Response{}, fmt.Errorf("call %s: %w", baseURL+path, err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxUpstreamBody))
	if err != nil {
		return Response{}, fmt.Errorf("read answer from %s: %w", baseURL+path, err)
	}
	return Response{Status: response.StatusCode, Header: response.Header, Body: payload}, nil
}

// Forward passes one of identity's four auth requests through unchanged: the
// inbound Cookie and Origin headers on the way in, and whatever came back on
// the way out.
// These four are the only place the gateway carries a cookie, and it sets none
// of its own: the whole session rule belongs to identity (spec 0001, the gateway
// holds no business rule).
// The path is a constant chosen by the route, never the inbound one, so nothing
// a caller sends decides where the gateway calls: only the query string travels,
// which is what Google's callback carries.
func (c *Client) Forward(ctx context.Context, r *http.Request, baseURL, path string) (Response, error) {
	target, err := urlpkg.Parse(baseURL)
	if err != nil {
		return Response{}, fmt.Errorf("parse upstream address: %w", err)
	}
	target.Path += path
	target.RawQuery = r.URL.RawQuery

	request, err := http.NewRequestWithContext(ctx, r.Method, target.String(), http.NoBody)
	if err != nil {
		return Response{}, fmt.Errorf("build request: %w", err)
	}
	if cookie := r.Header.Get("Cookie"); cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		request.Header.Set("Origin", origin)
	}
	if requestID := vermouth.RequestID(ctx); requestID != "" {
		request.Header.Set(vermouth.RequestIDHeader, requestID)
	}

	//nolint:gosec // G704: the address is configuration plus a path this route fixes; only the query travels, and a query cannot change the host.
	response, err := c.http.Do(request)
	if err != nil {
		return Response{}, fmt.Errorf("call %s: %w", baseURL+path, err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxUpstreamBody))
	if err != nil {
		return Response{}, fmt.Errorf("read answer from %s: %w", baseURL+path, err)
	}
	return Response{Status: response.StatusCode, Header: response.Header, Body: payload}, nil
}
