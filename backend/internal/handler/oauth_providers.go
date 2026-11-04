package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Profile is the identity a provider vouches for.
type Profile struct {
	Provider, SSOID, Email, Name, AvatarURL string
}

// OAuthProvider hides the provider-specific half of the authorization-code flow.
type OAuthProvider interface {
	AuthURL(state, redirectURL string) string
	Exchange(ctx context.Context, code, redirectURL string) (Profile, error)
}

var providerClient = &http.Client{Timeout: 10 * time.Second}

func exchangeToken(ctx context.Context, c *http.Client, tokenURL string, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := doJSON(c, req, &out); err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("token exchange: no access token (%s)", out.Error)
	}
	return out.AccessToken, nil
}

func getJSON(ctx context.Context, c *http.Client, rawURL, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return doJSON(c, req, out)
}

func doJSON(c *http.Client, req *http.Request, out any) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %d", req.URL.Host, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ---- Google ----

type googleProvider struct {
	clientID, clientSecret, tokenURL, userinfoURL string
	client                                        *http.Client
}

func NewGoogleProvider(clientID, secret string) OAuthProvider {
	return &googleProvider{
		clientID: clientID, clientSecret: secret,
		tokenURL:    "https://oauth2.googleapis.com/token",
		userinfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		client:      providerClient,
	}
}

func (g *googleProvider) AuthURL(state, redirectURL string) string {
	q := url.Values{
		"client_id": {g.clientID}, "redirect_uri": {redirectURL}, "response_type": {"code"},
		"scope": {"openid email profile"}, "state": {state},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
}

func (g *googleProvider) Exchange(ctx context.Context, code, redirectURL string) (Profile, error) {
	token, err := exchangeToken(ctx, g.client, g.tokenURL, url.Values{
		"code": {code}, "client_id": {g.clientID}, "client_secret": {g.clientSecret},
		"redirect_uri": {redirectURL}, "grant_type": {"authorization_code"},
	})
	if err != nil {
		return Profile{}, err
	}
	var u struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := getJSON(ctx, g.client, g.userinfoURL, token, &u); err != nil {
		return Profile{}, err
	}
	if u.Sub == "" || u.Email == "" || !u.EmailVerified {
		return Profile{}, errors.New("google account has no verified email")
	}
	return Profile{Provider: "google", SSOID: u.Sub, Email: u.Email, Name: u.Name, AvatarURL: u.Picture}, nil
}
