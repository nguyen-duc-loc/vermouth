// Command s3probe proves that the Garage billing credentials can read and write one private object.
//
//nolint:err113,mnd,noinlineerr // This one shot probe reports the exact failed S3 operation and status.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	s3Service        = "s3"
)

var runIDPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

type config struct {
	endpoint  *urlpkg.URL
	region    string
	bucket    string
	accessKey string
	secretKey string
	runID     string
}

type probe struct {
	client *http.Client
	now    time.Time
	config config
}

type operation struct {
	method         string
	objectPath     string
	expectedStatus int
}

func main() {
	client := &http.Client{Timeout: 10 * time.Second}
	if err := run(context.Background(), client, time.Now); err != nil {
		fmt.Fprintf(os.Stderr, "s3probe: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, client *http.Client, now func() time.Time) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	objectPath := "/" + urlpkg.PathEscape(cfg.bucket) + "/platform-probe/" + urlpkg.PathEscape(cfg.runID)
	s3 := probe{
		client: client,
		now:    now(),
		config: cfg,
	}

	if err := s3.send(ctx, operation{
		method:         http.MethodPut,
		objectPath:     objectPath,
		expectedStatus: http.StatusOK,
	}); err != nil {
		return fmt.Errorf("put probe object: %w", err)
	}
	if err := s3.send(ctx, operation{
		method:         http.MethodGet,
		objectPath:     objectPath,
		expectedStatus: http.StatusOK,
	}); err != nil {
		return fmt.Errorf("get probe object: %w", err)
	}
	if err := s3.send(ctx, operation{
		method:         http.MethodDelete,
		objectPath:     objectPath,
		expectedStatus: http.StatusNoContent,
	}); err != nil {
		return fmt.Errorf("delete probe object: %w", err)
	}
	if err := s3.send(ctx, operation{
		method:         http.MethodGet,
		objectPath:     objectPath,
		expectedStatus: http.StatusNotFound,
	}); err != nil {
		return fmt.Errorf("confirm probe cleanup: %w", err)
	}
	return nil
}

func loadConfig() (config, error) {
	required := []string{
		"GARAGE_S3_ENDPOINT",
		"GARAGE_S3_REGION",
		"GARAGE_BUCKET",
		"GARAGE_ACCESS_KEY_ID",
		"GARAGE_SECRET_ACCESS_KEY",
		"VERMOUTH_RUN_ID",
	}
	values := make(map[string]string, len(required))
	for _, name := range required {
		value := os.Getenv(name)
		if strings.TrimSpace(value) == "" {
			return config{}, fmt.Errorf("%s is required", name)
		}
		values[name] = value
	}
	endpoint, err := urlpkg.Parse(values["GARAGE_S3_ENDPOINT"])
	if err != nil {
		return config{}, fmt.Errorf("parse GARAGE_S3_ENDPOINT: %w", err)
	}
	if endpoint.Scheme != "http" || endpoint.Host != "garage:3900" ||
		(endpoint.Path != "" && endpoint.Path != "/") ||
		endpoint.RawQuery != "" ||
		endpoint.Fragment != "" {
		return config{}, errors.New("GARAGE_S3_ENDPOINT must be exactly http://garage:3900")
	}
	if values["GARAGE_S3_REGION"] != "garage" {
		return config{}, errors.New("GARAGE_S3_REGION must be garage")
	}
	if values["GARAGE_BUCKET"] != "vermouth-invoices" {
		return config{}, errors.New("GARAGE_BUCKET must be vermouth-invoices")
	}
	if !runIDPattern.MatchString(values["VERMOUTH_RUN_ID"]) {
		return config{}, errors.New("VERMOUTH_RUN_ID must contain 12 lowercase hexadecimal characters")
	}
	return config{
		endpoint:  endpoint,
		region:    values["GARAGE_S3_REGION"],
		bucket:    values["GARAGE_BUCKET"],
		accessKey: values["GARAGE_ACCESS_KEY_ID"],
		secretKey: values["GARAGE_SECRET_ACCESS_KEY"],
		runID:     values["VERMOUTH_RUN_ID"],
	}, nil
}

func (probe probe) send(ctx context.Context, operation operation) error {
	requestURL := *probe.config.endpoint
	requestURL.Path = operation.objectPath
	request, err := http.NewRequestWithContext(
		ctx,
		operation.method,
		requestURL.String(),
		bytes.NewReader(nil),
	)
	if err != nil {
		return fmt.Errorf("create %s request: %w", operation.method, err)
	}
	signRequest(request, probe.now.UTC(), probe.config, operation.objectPath)

	response, err := probe.client.Do(request)
	if err != nil {
		return fmt.Errorf("send %s request: %w", operation.method, err)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return fmt.Errorf("read and close %s response: %w", operation.method, err)
	}
	statusMatches := response.StatusCode == operation.expectedStatus
	deleteCompatibility := operation.method == http.MethodDelete &&
		operation.expectedStatus == http.StatusNoContent &&
		response.StatusCode == http.StatusOK
	if !statusMatches && !deleteCompatibility {
		return fmt.Errorf(
			"%s returned HTTP %d: %s",
			operation.method,
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	if operation.method == http.MethodGet && operation.expectedStatus == http.StatusOK && len(body) != 0 {
		return fmt.Errorf("GET returned %d bytes, expected a zero byte object", len(body))
	}
	return nil
}

func signRequest(request *http.Request, now time.Time, cfg config, canonicalPath string) {
	amzDate := now.Format("20060102T150405Z")
	shortDate := now.Format("20060102")
	request.Header.Set("X-Amz-Content-Sha256", emptyPayloadHash)
	request.Header.Set("X-Amz-Date", amzDate)

	canonicalHeaders := "host:" + request.URL.Host + "\n" +
		"x-amz-content-sha256:" + emptyPayloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		request.Method,
		canonicalPath,
		"",
		canonicalHeaders,
		signedHeaders,
		emptyPayloadHash,
	}, "\n")
	scope := strings.Join([]string{shortDate, cfg.region, s3Service, "aws4_request"}, "/")
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(requestHash[:])

	dateKey := hmacSHA256([]byte("AWS4"+cfg.secretKey), shortDate)
	regionKey := hmacSHA256(dateKey, cfg.region)
	serviceKey := hmacSHA256(regionKey, s3Service)
	signingKey := hmacSHA256(serviceKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	request.Header.Set(
		"Authorization",
		"AWS4-HMAC-SHA256 Credential="+cfg.accessKey+"/"+scope+
			", SignedHeaders="+signedHeaders+
			", Signature="+signature,
	)
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	if _, err := mac.Write([]byte(value)); err != nil {
		panic("sha256 hash write failed: " + err.Error())
	}
	return mac.Sum(nil)
}
