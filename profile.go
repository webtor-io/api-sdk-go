package webtor

import (
	"context"
	"encoding/json"
	"net/http"
)

// Profile returns the authenticated account. Web-ui backend only.
func (c *Client) Profile(ctx context.Context) (*ProfileResponse, error) {
	if err := c.require(CapProfile); err != nil {
		return nil, err
	}
	var out ProfileResponse
	if err := c.do(ctx, apiRequest{method: http.MethodGet, path: "profile"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ProfileUpdate carries the updatable account settings. Nil fields are left
// untouched.
type ProfileUpdate struct {
	ShowAdult *bool `json:"show_adult,omitempty"`
}

// UpdateProfile patches account settings and returns the updated profile.
func (c *Client) UpdateProfile(ctx context.Context, u ProfileUpdate) (*ProfileResponse, error) {
	if err := c.require(CapProfile); err != nil {
		return nil, err
	}
	body, err := json.Marshal(u)
	if err != nil {
		return nil, err
	}
	var out ProfileResponse
	err = c.do(ctx, apiRequest{
		method:      http.MethodPatch,
		path:        "profile",
		body:        body,
		contentType: "application/json",
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
