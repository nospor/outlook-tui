package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

type TokenCache struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

func GetTokenCachePath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token.json"), nil
}

func LoadToken() (TokenCache, error) {
	path, err := GetTokenCachePath()
	if err != nil {
		return TokenCache{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return TokenCache{}, err
	}
	var t TokenCache
	err = json.Unmarshal(data, &t)
	return t, err
}

func SaveToken(t TokenCache) error {
	path, err := GetTokenCachePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func RequestDeviceCode(clientID, tenantID string) (*DeviceCodeResponse, error) {
	endpoint := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/devicecode", tenantID)
	
	form := url.Values{}
	form.Add("client_id", clientID)
	form.Add("scope", "offline_access User.Read Mail.ReadWrite Mail.Send")
	
	resp, err := http.PostForm(endpoint, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("devicecode endpoint returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var deviceResp DeviceCodeResponse
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		return nil, err
	}
	return &deviceResp, nil
}

func PollForToken(clientID, tenantID string, deviceResp *DeviceCodeResponse, updateProgress func(string)) (TokenCache, error) {
	endpoint := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenantID)
	interval := time.Duration(deviceResp.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}
	
	expiry := time.Now().Add(time.Duration(deviceResp.ExpiresIn) * time.Second)
	
	for time.Now().Before(expiry) {
		form := url.Values{}
		form.Add("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Add("client_id", clientID)
		form.Add("device_code", deviceResp.DeviceCode)
		
		resp, err := http.PostForm(endpoint, form)
		if err != nil {
			return TokenCache{}, err
		}
		
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return TokenCache{}, err
		}
		
		if resp.StatusCode == http.StatusOK {
			var tokenResp TokenResponse
			if err := json.Unmarshal(body, &tokenResp); err != nil {
				return TokenCache{}, err
			}
			tc := TokenCache{
				AccessToken:  tokenResp.AccessToken,
				RefreshToken: tokenResp.RefreshToken,
				Expiry:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
			}
			return tc, nil
		}
		
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if err := json.Unmarshal(body, &errResp); err == nil {
			if errResp.Error == "authorization_pending" {
				time.Sleep(interval)
				continue
			} else if errResp.Error == "authorization_declined" {
				return TokenCache{}, errors.New("authorization declined by the user")
			} else if errResp.Error == "expired_token" {
				return TokenCache{}, errors.New("the device code has expired")
			} else {
				return TokenCache{}, fmt.Errorf("oauth error: %s - %s", errResp.Error, errResp.ErrorDescription)
			}
		}
		
		return TokenCache{}, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}
	
	return TokenCache{}, errors.New("device code expired")
}

func RefreshToken(clientID, tenantID, refreshToken string) (TokenCache, error) {
	endpoint := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenantID)
	
	form := url.Values{}
	form.Add("grant_type", "refresh_token")
	form.Add("client_id", clientID)
	form.Add("refresh_token", refreshToken)
	form.Add("scope", "offline_access User.Read Mail.ReadWrite Mail.Send")
	
	resp, err := http.PostForm(endpoint, form)
	if err != nil {
		return TokenCache{}, err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TokenCache{}, err
	}
	
	if resp.StatusCode != http.StatusOK {
		return TokenCache{}, fmt.Errorf("token refresh returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return TokenCache{}, err
	}
	
	tc := TokenCache{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	return tc, nil
}

type Authenticator struct {
	ClientID string
	TenantID string
	token    TokenCache
}

func NewAuthenticator(clientID, tenantID string, token TokenCache) *Authenticator {
	return &Authenticator{
		ClientID: clientID,
		TenantID: tenantID,
		token:    token,
	}
}

func (a *Authenticator) GetClient() *http.Client {
	return &http.Client{
		Transport: &oauthTransport{
			authenticator: a,
			base:          http.DefaultTransport,
		},
	}
}

type oauthTransport struct {
	authenticator *Authenticator
	base          http.RoundTripper
}

func (t *oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Add 5 minutes buffer for expiry check
	if time.Now().Add(5 * time.Minute).After(t.authenticator.token.Expiry) {
		if t.authenticator.token.RefreshToken == "" {
			return nil, errors.New("token expired and no refresh token available")
		}
		newToken, err := RefreshToken(t.authenticator.ClientID, t.authenticator.TenantID, t.authenticator.token.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh token: %w", err)
		}
		t.authenticator.token = newToken
		if err := SaveToken(newToken); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to cache refreshed token: %v\n", err)
		}
	}
	
	// Add Authorization header
	req.Header.Set("Authorization", "Bearer "+t.authenticator.token.AccessToken)
	return t.base.RoundTrip(req)
}
