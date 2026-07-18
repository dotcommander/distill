package codexresets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
)

const (
	defaultEndpoint = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	maxResponseBody = 1 << 20
)

type Options struct {
	AuthPath string
	Endpoint string
	Now      func() time.Time
	Client   *http.Client
}

type authFile struct {
	Tokens authTokens `json:"tokens"`

	AccessToken      string `json:"access_token"`
	AccountID        string `json:"account_id"`
	ChatGPTAccountID string `json:"chatgpt_account_id"`
}

type authTokens struct {
	AccessToken      string `json:"access_token"`
	AccountID        string `json:"account_id"`
	ChatGPTAccountID string `json:"chatgpt_account_id"`
}

type responsePayload struct {
	AvailableCount *int              `json:"available_count"`
	Credits        []json.RawMessage `json:"credits"`
	Data           []json.RawMessage `json:"data"`
	Items          []json.RawMessage `json:"items"`
}

type creditFields struct {
	ExpiresAt    expiryTime `json:"expires_at"`
	ExpiresAtAlt expiryTime `json:"expiresAt"`
	Expiry       expiryTime `json:"expiry"`
	Expires      expiryTime `json:"expires"`
	Expiration   expiryTime `json:"expiration"`
	ExpirationAt expiryTime `json:"expiration_at"`
}

type expiryTime struct {
	Time time.Time
	OK   bool
}

func (e *expiryTime) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var number float64
	if err := json.Unmarshal(data, &number); err == nil {
		seconds := number
		if number > 10_000_000_000 {
			seconds = number / 1000
		}
		e.Time = time.Unix(int64(seconds), int64((seconds-float64(int64(seconds)))*1e9)).UTC()
		e.OK = true
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode expiry time: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.HasSuffix(value, "Z") {
		value = strings.TrimSuffix(value, "Z") + "+00:00"
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fmt.Errorf("parse expiry time %q: %w", value, err)
	}
	e.Time = parsed
	e.OK = true
	return nil
}

func Run(ctx context.Context, out io.Writer, opts Options) error {
	if opts.AuthPath == "" {
		authPath, err := defaultAuthPath()
		if err != nil {
			return err
		}
		opts.AuthPath = authPath
	}
	if opts.Endpoint == "" {
		opts.Endpoint = defaultEndpoint
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 30 * time.Second}
	}

	token, accountID, err := readAuth(opts.AuthPath)
	if err != nil {
		return err
	}
	payload, err := fetch(ctx, opts.Client, opts.Endpoint, token, accountID)
	if err != nil {
		return err
	}
	return writeReport(out, payload, opts.Now())
}

func defaultAuthPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".codex", "auth.json"), nil
}

func readAuth(path string) (accessToken string, accountID string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read auth file %s: %w", path, err)
	}
	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return "", "", fmt.Errorf("decode auth file %s: %w", path, err)
	}

	accessToken = auth.Tokens.AccessToken
	accountID = firstNonEmpty(auth.Tokens.AccountID, auth.Tokens.ChatGPTAccountID)
	if accessToken == "" {
		accessToken = auth.AccessToken
	}
	if accountID == "" {
		accountID = firstNonEmpty(auth.AccountID, auth.ChatGPTAccountID)
	}
	if accessToken == "" {
		return "", "", fmt.Errorf("no access_token found in %s", path)
	}
	return accessToken, accountID, nil
}

func fetch(ctx context.Context, client *http.Client, endpoint string, accessToken string, accountID string) (responsePayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return responsePayload{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("OpenAI-Beta", "codex-1")
	req.Header.Set("User-Agent", "codex-resets/1.0")
	req.Header.Set("originator", "Codex Desktop")
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return responsePayload{}, fmt.Errorf("request reset credits: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return responsePayload{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responsePayload{}, fmt.Errorf("request reset credits: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload responsePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return responsePayload{}, fmt.Errorf("decode response: %w", err)
	}
	return payload, nil
}

func writeReport(out io.Writer, payload responsePayload, now time.Time) error {
	credits, err := parseCredits(payload)
	if err != nil {
		return err
	}

	count := len(credits)
	if payload.AvailableCount != nil {
		count = *payload.AvailableCount
	}
	label := "resets"
	if count == 1 {
		label = "reset"
	}

	checked := now.Local().Format("Jan 2, 3:04 PM MST")
	_, _ = fmt.Fprintln(out, "ChatGPT / Codex Reset Credits")
	_, _ = fmt.Fprintf(out, "Available: %d %s\n", count, label)
	_, _ = fmt.Fprintf(out, "Checked: %s\n\n", checked)
	if len(credits) == 0 {
		_, _ = fmt.Fprintln(out, "No reset credits found.")
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "#\tExpires\tTime left")
	_, _ = fmt.Fprintln(tw, "-\t-------\t---------")
	for i, credit := range credits {
		expires, left := "unknown", "unknown"
		if credit.OK {
			expires = credit.Time.Local().Format("Jan 2, 3:04 PM")
			left = timeLeft(now, credit.Time)
		}
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\n", i+1, expires, left)
	}
	return tw.Flush()
}

func parseCredits(payload responsePayload) ([]expiryTime, error) {
	rawCredits := payload.Credits
	if rawCredits == nil {
		rawCredits = payload.Data
	}
	if rawCredits == nil {
		rawCredits = payload.Items
	}

	credits := make([]expiryTime, 0, len(rawCredits))
	for i, raw := range rawCredits {
		var fields creditFields
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("decode credit %d: %w", i+1, err)
		}
		credits = append(credits, firstExpiry(fields))
	}
	return credits, nil
}

func firstExpiry(fields creditFields) expiryTime {
	for _, candidate := range []expiryTime{
		fields.ExpiresAt,
		fields.ExpiresAtAlt,
		fields.Expiry,
		fields.Expires,
		fields.Expiration,
		fields.ExpirationAt,
	} {
		if candidate.OK {
			return candidate
		}
	}
	return expiryTime{}
}

func timeLeft(now time.Time, expires time.Time) string {
	remaining := expires.Sub(now)
	if remaining <= 0 {
		return "expired"
	}

	totalSeconds := int(remaining.Seconds())
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	minutes := (totalSeconds % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
