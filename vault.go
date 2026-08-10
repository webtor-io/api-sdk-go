package webtor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Vault returns the account's Vault state: point balance, content counters
// and all pledges. Web-ui backend only.
func (c *Client) Vault(ctx context.Context) (*VaultResponse, error) {
	if err := c.require(CapVault); err != nil {
		return nil, err
	}
	var out VaultResponse
	if err := c.do(ctx, apiRequest{method: http.MethodGet, path: "vault"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VaultPledge pledges Vault points to keep a resource stored long-term
// (1 point per GB). A payment_required/forbidden error means insufficient
// points; a conflict error means a pledge already exists.
func (c *Client) VaultPledge(ctx context.Context, resourceID string) (*Pledge, error) {
	if err := c.require(CapVault); err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]string{"resource_id": normalizeResourceID(resourceID)})
	if err != nil {
		return nil, err
	}
	var out Pledge
	err = c.do(ctx, apiRequest{
		method:      http.MethodPost,
		path:        "vault/pledges",
		body:        body,
		contentType: "application/json",
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// VaultPledgeStatus returns a pledge and where its transfer stands. Poll it
// every 10–30 seconds while waiting for a transfer; a failed status is
// terminal for the attempt, not the resource — storage retries on its own
// schedule, so keep polling instead of re-pledging.
func (c *Client) VaultPledgeStatus(ctx context.Context, resourceID string) (*PledgeStatusResponse, error) {
	if err := c.require(CapVault); err != nil {
		return nil, err
	}
	var out PledgeStatusResponse
	err := c.do(ctx, apiRequest{
		method: http.MethodGet,
		path:   "vault/pledges/" + url.PathEscape(normalizeResourceID(resourceID)),
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// VaultUnpledge withdraws a pledge and claims its points back. A conflict
// error means the pledge is frozen and cannot be withdrawn yet.
func (c *Client) VaultUnpledge(ctx context.Context, resourceID string) error {
	if err := c.require(CapVault); err != nil {
		return err
	}
	return c.do(ctx, apiRequest{
		method: http.MethodDelete,
		path:   "vault/pledges/" + url.PathEscape(normalizeResourceID(resourceID)),
	}, nil)
}
