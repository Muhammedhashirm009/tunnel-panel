package tunnel

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const cloudflareAPI = "https://api.cloudflare.com/client/v4"

// CloudflareClient handles all Cloudflare API interactions
type CloudflareClient struct {
	apiToken  string
	accountID string
	zoneID    string
	zoneName  string
	client    *http.Client
}

// NewCloudflareClient creates a new Cloudflare API client
func NewCloudflareClient(apiToken, accountID, zoneID, zoneName string) *CloudflareClient {
	return &CloudflareClient{
		apiToken:  apiToken,
		accountID: accountID,
		zoneID:    zoneID,
		zoneName:  zoneName,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// --- API Request Helpers ---

type cfResponse struct {
	Success  bool            `json:"success"`
	Errors   []cfError       `json:"errors"`
	Messages []string        `json:"messages"`
	Result   json.RawMessage `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *CloudflareClient) doRequest(method, path string, body interface{}) (*cfResponse, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, cloudflareAPI+path, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare API request failed: %w", err)
	}
	defer resp.Body.Close()

	var cfResp cfResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !cfResp.Success {
		if len(cfResp.Errors) > 0 {
			return nil, fmt.Errorf("cloudflare API error: %s (code %d)", cfResp.Errors[0].Message, cfResp.Errors[0].Code)
		}
		return nil, fmt.Errorf("cloudflare API returned unsuccessful response")
	}

	return &cfResp, nil
}

// --- Zone Operations ---

// Zone represents a Cloudflare zone
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListZones returns all zones in the account
func (c *CloudflareClient) ListZones() ([]Zone, error) {
	resp, err := c.doRequest("GET", "/zones?per_page=50", nil)
	if err != nil {
		return nil, err
	}

	var zones []Zone
	if err := json.Unmarshal(resp.Result, &zones); err != nil {
		return nil, err
	}
	return zones, nil
}

// --- DNS Operations ---

// DNSRecord represents a Cloudflare DNS record
type DNSRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

// CreateDNSRecord creates a CNAME record pointing to the tunnel
func (c *CloudflareClient) CreateDNSRecord(subdomain, tunnelID string) (*DNSRecord, error) {
	record := DNSRecord{
		Type:    "CNAME",
		Name:    subdomain,
		Content: fmt.Sprintf("%s.cfargotunnel.com", tunnelID),
		TTL:     1, // Auto
		Proxied: true,
	}

	resp, err := c.doRequest("POST", fmt.Sprintf("/zones/%s/dns_records", c.zoneID), record)
	if err != nil {
		return nil, err
	}

	var created DNSRecord
	if err := json.Unmarshal(resp.Result, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// DeleteDNSRecord removes a DNS record by ID
func (c *CloudflareClient) DeleteDNSRecord(recordID string) error {
	_, err := c.doRequest("DELETE", fmt.Sprintf("/zones/%s/dns_records/%s", c.zoneID, recordID), nil)
	return err
}

// GetDNSRecordByName finds a DNS record by name
func (c *CloudflareClient) GetDNSRecordByName(name string) (*DNSRecord, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/zones/%s/dns_records?name=%s&type=CNAME", c.zoneID, name), nil)
	if err != nil {
		return nil, err
	}

	var records []DNSRecord
	if err := json.Unmarshal(resp.Result, &records); err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("DNS record not found: %s", name)
	}
	return &records[0], nil
}

// --- Tunnel Operations ---

// Tunnel represents a Cloudflare tunnel
type Tunnel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateTunnel creates a new Cloudflare tunnel
func (c *CloudflareClient) CreateTunnel(name string) (*Tunnel, string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, "", fmt.Errorf("failed to generate tunnel secret: %w", err)
	}
	secretB64 := base64.StdEncoding.EncodeToString(secret)

	body := map[string]interface{}{
		"name":          name,
		"tunnel_secret": secretB64,
		"config_src":    "local",
	}

	resp, err := c.doRequest("POST", fmt.Sprintf("/accounts/%s/cfd_tunnel", c.accountID), body)
	if err != nil {
		return nil, "", err
	}

	var tunnel Tunnel
	if err := json.Unmarshal(resp.Result, &tunnel); err != nil {
		return nil, "", err
	}

	return &tunnel, secretB64, nil
}

// DeleteTunnel deletes a Cloudflare tunnel
func (c *CloudflareClient) DeleteTunnel(tunnelID string) error {
	_, err := c.doRequest("DELETE", fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", c.accountID, tunnelID), nil)
	return err
}

// GetTunnel retrieves tunnel information
func (c *CloudflareClient) GetTunnel(tunnelID string) (*Tunnel, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", c.accountID, tunnelID), nil)
	if err != nil {
		return nil, err
	}

	var tunnel Tunnel
	if err := json.Unmarshal(resp.Result, &tunnel); err != nil {
		return nil, err
	}
	return &tunnel, nil
}

// ListTunnels returns all tunnels for the account
func (c *CloudflareClient) ListTunnels() ([]Tunnel, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/accounts/%s/cfd_tunnel?is_deleted=false", c.accountID), nil)
	if err != nil {
		return nil, err
	}

	var tunnels []Tunnel
	if err := json.Unmarshal(resp.Result, &tunnels); err != nil {
		return nil, err
	}
	return tunnels, nil
}

// VerifyToken checks if the API token is valid
func (c *CloudflareClient) VerifyToken() error {
	_, err := c.doRequest("GET", "/user/tokens/verify", nil)
	return err
}
