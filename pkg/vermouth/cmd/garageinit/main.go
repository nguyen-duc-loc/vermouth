// Command garageinit reconciles the one node local Garage control plane through its Admin API.
//
//nolint:err113,mnd,noinlineerr,tagliatelle // Garage's exact API and local contract need specific boundary errors, JSON names, and HTTP.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	desiredCapacity = int64(5_000_000_000)
	maxResponseSize = 1 << 20
)

var nodeIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type config struct {
	endpoint  *urlpkg.URL
	token     string
	zone      string
	bucket    string
	keyName   string
	accessKey string
	secretKey string
}

type adminClient struct {
	httpClient *http.Client
	endpoint   *urlpkg.URL
	token      string
}

type nodeRole struct {
	Zone     string   `json:"zone"`
	Capacity *int64   `json:"capacity"`
	Tags     []string `json:"tags"`
}

type clusterNode struct {
	ID   string    `json:"id"`
	Role *nodeRole `json:"role"`
}

type clusterStatus struct {
	LayoutVersion int64         `json:"layoutVersion"`
	Nodes         []clusterNode `json:"nodes"`
}

type roleChange struct {
	ID       string   `json:"id"`
	Zone     string   `json:"zone"`
	Capacity *int64   `json:"capacity"`
	Tags     []string `json:"tags"`
}

type clusterLayout struct {
	Version           int64        `json:"version"`
	Roles             []roleChange `json:"roles"`
	StagedRoleChanges []roleChange `json:"stagedRoleChanges"`
}

type bucketPermission struct {
	Read  bool `json:"read"`
	Write bool `json:"write"`
	Owner bool `json:"owner"`
}

type bucketKey struct {
	AccessKeyID string           `json:"accessKeyId"`
	Name        string           `json:"name"`
	Permissions bucketPermission `json:"permissions"`
}

type bucketInfo struct {
	ID            string      `json:"id"`
	GlobalAliases []string    `json:"globalAliases"`
	Keys          []bucketKey `json:"keys"`
}

type keySummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type keyInfo struct {
	AccessKeyID string `json:"accessKeyId"`
	Name        string `json:"name"`
	Expired     bool   `json:"expired"`
}

func main() {
	if err := runMain(); err != nil {
		fmt.Fprintf(os.Stderr, "garageinit: %v\n", err)
		os.Exit(1)
	}
}

func runMain() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := &adminClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		endpoint:   cfg.endpoint,
		token:      cfg.token,
	}
	return run(ctx, client, cfg)
}

func loadConfig() (config, error) {
	required := []string{
		"GARAGE_ADMIN_ENDPOINT",
		"GARAGE_ADMIN_TOKEN",
		"GARAGE_ZONE",
		"GARAGE_BUCKET",
		"GARAGE_KEY_NAME",
		"GARAGE_ACCESS_KEY_ID",
		"GARAGE_SECRET_ACCESS_KEY",
	}
	values := make(map[string]string, len(required))
	for _, name := range required {
		value := os.Getenv(name)
		if strings.TrimSpace(value) == "" {
			return config{}, fmt.Errorf("%s is required", name)
		}
		values[name] = value
	}
	endpoint, err := urlpkg.Parse(values["GARAGE_ADMIN_ENDPOINT"])
	if err != nil {
		return config{}, fmt.Errorf("parse GARAGE_ADMIN_ENDPOINT: %w", err)
	}
	//nolint:revive // The cluster local Garage admin endpoint intentionally has no TLS.
	if endpoint.String() != "http://garage:3903" {
		return config{}, errors.New("GARAGE_ADMIN_ENDPOINT must be exactly http://garage:3903")
	}
	if values["GARAGE_ZONE"] != "local" || values["GARAGE_BUCKET"] != "vermouth-invoices" ||
		values["GARAGE_KEY_NAME"] != "billing" {
		return config{}, errors.New("Garage zone, bucket, and key name differ from the local contract")
	}
	return config{
		endpoint:  endpoint,
		token:     values["GARAGE_ADMIN_TOKEN"],
		zone:      values["GARAGE_ZONE"],
		bucket:    values["GARAGE_BUCKET"],
		keyName:   values["GARAGE_KEY_NAME"],
		accessKey: values["GARAGE_ACCESS_KEY_ID"],
		secretKey: values["GARAGE_SECRET_ACCESS_KEY"],
	}, nil
}

func run(ctx context.Context, client *adminClient, cfg config) error {
	if err := reconcileLayout(ctx, client, cfg); err != nil {
		return fmt.Errorf("reconcile layout: %w", err)
	}
	bucket, err := reconcileBucket(ctx, client, cfg.bucket)
	if err != nil {
		return fmt.Errorf("reconcile bucket: %w", err)
	}
	if err := reconcileKey(ctx, client, cfg); err != nil {
		return fmt.Errorf("reconcile key: %w", err)
	}
	if err := reconcilePermission(ctx, client, bucket.ID, cfg); err != nil {
		return fmt.Errorf("reconcile bucket permission: %w", err)
	}
	return nil
}

func reconcileLayout(ctx context.Context, client *adminClient, cfg config) error {
	status := clusterStatus{Nodes: []clusterNode{}}
	if _, err := client.do(ctx, http.MethodGet, "/v2/GetClusterStatus", nil, &status); err != nil {
		return err
	}
	if len(status.Nodes) != 1 || !nodeIDPattern.MatchString(status.Nodes[0].ID) {
		return fmt.Errorf("cluster reports %d nodes instead of one full node id", len(status.Nodes))
	}
	node := status.Nodes[0]
	if node.Role != nil {
		return verifyRole(node.ID, *node.Role, cfg)
	}

	layout := clusterLayout{Roles: []roleChange{}, StagedRoleChanges: []roleChange{}}
	if _, err := client.do(ctx, http.MethodGet, "/v2/GetClusterLayout", nil, &layout); err != nil {
		return err
	}
	if len(layout.StagedRoleChanges) == 0 {
		capacity := new(int64)
		*capacity = desiredCapacity
		change := roleChange{ID: node.ID, Zone: cfg.zone, Capacity: capacity, Tags: []string{}}
		request := struct {
			Roles []roleChange `json:"roles"`
		}{Roles: []roleChange{change}}
		if _, err := client.do(ctx, http.MethodPost, "/v2/UpdateClusterLayout", request, &layout); err != nil {
			return err
		}
	}
	if len(layout.StagedRoleChanges) != 1 {
		return fmt.Errorf("layout has %d staged role changes", len(layout.StagedRoleChanges))
	}
	if err := verifyRole(node.ID, roleFromChange(layout.StagedRoleChanges[0]), cfg); err != nil {
		return fmt.Errorf("staged role: %w", err)
	}
	apply := struct {
		Version int64 `json:"version"`
	}{Version: layout.Version + 1}
	if _, err := client.do(ctx, http.MethodPost, "/v2/ApplyClusterLayout", apply, &layout); err != nil {
		return err
	}

	status = clusterStatus{Nodes: []clusterNode{}}
	if _, err := client.do(ctx, http.MethodGet, "/v2/GetClusterStatus", nil, &status); err != nil {
		return err
	}
	if len(status.Nodes) != 1 || status.Nodes[0].Role == nil {
		return errors.New("applied layout did not assign the one node")
	}
	return verifyRole(status.Nodes[0].ID, *status.Nodes[0].Role, cfg)
}

func verifyRole(nodeID string, role nodeRole, cfg config) error {
	if !nodeIDPattern.MatchString(nodeID) || role.Zone != cfg.zone || role.Capacity == nil ||
		*role.Capacity != desiredCapacity || len(role.Tags) != 0 {
		return errors.New("Garage node has a conflicting zone, capacity, or tags")
	}
	return nil
}

func roleFromChange(change roleChange) nodeRole {
	return nodeRole{Zone: change.Zone, Capacity: change.Capacity, Tags: change.Tags}
}

func reconcileBucket(ctx context.Context, client *adminClient, name string) (bucketInfo, error) {
	query := urlpkg.Values{"globalAlias": []string{name}}
	path := "/v2/GetBucketInfo?" + query.Encode()
	bucket := bucketInfo{GlobalAliases: []string{}, Keys: []bucketKey{}}
	found, err := client.do(ctx, http.MethodGet, path, nil, &bucket)
	if err != nil {
		return bucketInfo{}, err
	}
	if !found {
		request := struct {
			GlobalAlias string `json:"globalAlias"`
		}{GlobalAlias: name}
		if _, err := client.do(ctx, http.MethodPost, "/v2/CreateBucket", request, &bucket); err != nil {
			return bucketInfo{}, err
		}
	}
	if bucket.ID == "" || !slices.Equal(bucket.GlobalAliases, []string{name}) {
		return bucketInfo{}, errors.New("Garage bucket identity or global aliases conflict")
	}
	return bucket, nil
}

func reconcileKey(ctx context.Context, client *adminClient, cfg config) error {
	keys := []keySummary{}
	if _, err := client.do(ctx, http.MethodGet, "/v2/ListKeys", nil, &keys); err != nil {
		return err
	}
	found := false
	for _, key := range keys {
		if key.Name == cfg.keyName && key.ID != cfg.accessKey {
			return errors.New("Garage key name belongs to a different access key")
		}
		if key.ID == cfg.accessKey {
			if key.Name != cfg.keyName {
				return errors.New("Garage access key belongs to a different name")
			}
			found = true
		}
	}
	if !found {
		request := struct {
			AccessKeyID     string `json:"accessKeyId"`
			SecretAccessKey string `json:"secretAccessKey"`
			Name            string `json:"name"`
		}{AccessKeyID: cfg.accessKey, SecretAccessKey: cfg.secretKey, Name: cfg.keyName}
		if _, err := client.do(ctx, http.MethodPost, "/v2/ImportKey", request, &keyInfo{}); err != nil {
			return err
		}
	}
	query := urlpkg.Values{"id": []string{cfg.accessKey}}
	key := keyInfo{}
	if _, err := client.do(ctx, http.MethodGet, "/v2/GetKeyInfo?"+query.Encode(), nil, &key); err != nil {
		return err
	}
	if key.AccessKeyID != cfg.accessKey || key.Name != cfg.keyName || key.Expired {
		return errors.New("Garage key identity, name, or expiration conflicts")
	}
	return nil
}

func reconcilePermission(ctx context.Context, client *adminClient, bucketID string, cfg config) error {
	bucket, err := getBucketByID(ctx, client, bucketID)
	if err != nil {
		return err
	}
	permission, found := permissionFor(bucket.Keys, cfg.accessKey)
	if found && permission.Owner {
		return errors.New("Garage billing key unexpectedly has owner permission")
	}
	if !found || !permission.Read || !permission.Write {
		request := struct {
			BucketID    string           `json:"bucketId"`
			AccessKeyID string           `json:"accessKeyId"`
			Permissions bucketPermission `json:"permissions"`
		}{
			BucketID:    bucketID,
			AccessKeyID: cfg.accessKey,
			Permissions: bucketPermission{Read: true, Write: true, Owner: false},
		}
		if _, err := client.do(ctx, http.MethodPost, "/v2/AllowBucketKey", request, &bucketInfo{}); err != nil {
			return err
		}
	}
	bucket, err = getBucketByID(ctx, client, bucketID)
	if err != nil {
		return err
	}
	permission, found = permissionFor(bucket.Keys, cfg.accessKey)
	if !found || !permission.Read || !permission.Write || permission.Owner {
		return errors.New("Garage billing key must have read and write without owner permission")
	}
	return nil
}

func getBucketByID(ctx context.Context, client *adminClient, id string) (bucketInfo, error) {
	query := urlpkg.Values{"id": []string{id}}
	bucket := bucketInfo{GlobalAliases: []string{}, Keys: []bucketKey{}}
	found, err := client.do(ctx, http.MethodGet, "/v2/GetBucketInfo?"+query.Encode(), nil, &bucket)
	if err != nil {
		return bucketInfo{}, err
	}
	if !found {
		return bucketInfo{}, errors.New("Garage bucket disappeared during reconciliation")
	}
	return bucket, nil
}

func permissionFor(keys []bucketKey, accessKey string) (bucketPermission, bool) {
	for _, key := range keys {
		if key.AccessKeyID == accessKey {
			return key.Permissions, true
		}
	}
	return bucketPermission{}, false
}

func (client *adminClient) do(
	ctx context.Context,
	method string,
	path string,
	requestBody any,
	responseBody any,
) (bool, error) {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return false, fmt.Errorf("encode %s %s request: %w", method, path, err)
		}
		body = bytes.NewReader(encoded)
	}
	requestPath, rawQuery, _ := strings.Cut(path, "?")
	requestURL := client.endpoint.ResolveReference(&urlpkg.URL{Path: requestPath})
	if rawQuery != "" {
		requestURL.RawQuery = rawQuery
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return false, fmt.Errorf("create %s %s request: %w", method, path, err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return false, fmt.Errorf("send %s %s request: %w", method, path, err)
	}
	content, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return false, fmt.Errorf("read and close %s %s response: %w", method, path, err)
	}
	if len(content) > maxResponseSize {
		return false, fmt.Errorf("%s %s response exceeds %d bytes", method, path, maxResponseSize)
	}
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%s %s returned HTTP %d", method, path, response.StatusCode)
	}
	if responseBody == nil {
		return true, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(responseBody); err != nil {
		return false, fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	return true, nil
}
