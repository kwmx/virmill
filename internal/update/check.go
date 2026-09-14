package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"virmill.local/core/internal/buildinfo"
)

// Repository is where Virmill releases are published.
const Repository = "kwmx/virmill"

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

type Release struct {
	Version    string    `json:"version"`
	Tag        string    `json:"tag"`
	Prerelease bool      `json:"prerelease"`
	Page       string    `json:"page"`
	Published  time.Time `json:"published"`
	Assets     []Asset   `json:"assets"`
}

// Result is the outcome of a check, as cached and shown. Latest is set only
// when a newer release exists.
type Result struct {
	Current   string    `json:"current"`
	Latest    *Release  `json:"latest,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
	Error     string    `json:"error,omitempty"`
}

func (r Result) Available() bool { return r.Latest != nil }

// Client reads the release list and downloads release files over HTTPS.
type Client struct {
	API   string
	Repo  string
	HTTP  *http.Client
	Allow func(*url.URL) error
}

// Default talks to GitHub and accepts only GitHub's API and download hosts.
func Default() Client {
	c := Client{API: "https://api.github.com", Repo: Repository, Allow: allowedURL}
	c.HTTP = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return c.Allow(req.URL)
	}}
	return c
}

func allowedURL(u *url.URL) error {
	host := u.Hostname()
	if u.Scheme == "https" && (host == "api.github.com" || host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com")) {
		return nil
	}
	return fmt.Errorf("refusing to download from %s: only GitHub over HTTPS is allowed", u.Redacted())
}

func (c Client) get(ctx context.Context, raw, accept string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if err := c.Allow(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "virmill/"+buildinfo.Version)
	req.Header.Set("Accept", accept)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub answered %s for %s", resp.Status, u.Redacted())
	}
	return resp, nil
}

// Check finds the newest release after current. Betas are offered only to
// users who run a beta; drafts and tags that are not Virmill versions are skipped.
func (c Client) Check(ctx context.Context, current string) (Result, error) {
	res := Result{Current: current, CheckedAt: time.Now().UTC()}
	cur, err := ParseVersion(current)
	if err != nil {
		return res, err
	}
	resp, err := c.get(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=30", c.API, c.Repo), "application/vnd.github+json")
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()
	var list []struct {
		Tag        string    `json:"tag_name"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
		Page       string    `json:"html_url"`
		Published  time.Time `json:"published_at"`
		Assets     []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&list); err != nil {
		return res, fmt.Errorf("unreadable release list: %w", err)
	}
	var best Version
	for _, r := range list {
		v, err := ParseVersion(r.Tag)
		if err != nil || r.Draft || (v.Prerelease() && !cur.Prerelease()) || !cur.Less(v) || (res.Latest != nil && !best.Less(v)) {
			continue
		}
		rel := Release{Version: v.String(), Tag: r.Tag, Prerelease: r.Prerelease, Page: r.Page, Published: r.Published, Assets: []Asset{}}
		for _, a := range r.Assets {
			rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
		}
		res.Latest, best = &rel, v
	}
	return res, nil
}
