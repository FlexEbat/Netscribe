// Package update finds out whether a newer release of Netscribe exists and installs it.
//
// It uses only the standard library. The release list is read from one fixed GitHub
// address, the answer is size-limited, and nothing from it is trusted beyond a version tag,
// a link on github.com/FlexEbat/Netscribe and a text that the interface shows escaped.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Repo is the only repository a release is read from.
	Repo = "FlexEbat/Netscribe"
	// LatestURL is the only address the check asks.
	LatestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"
	// ReleasesURL is the page that lists every release with its changelog.
	ReleasesURL = "https://github.com/" + Repo + "/releases"

	maxAnswer  = 1 << 20 // the release answer is read up to 1 MiB
	maxNotes   = 8000    // the changelog text kept for the interface, in bytes
	checkEvery = 6 * time.Hour
	askTimeout = 10 * time.Second
)

// Release is the newest published release.
type Release struct {
	Tag         string    `json:"tag"`     // v0.3.0
	Version     string    `json:"version"` // 0.3.0
	URL         string    `json:"url"`     // the release page
	Notes       string    `json:"notes"`   // the changelog of the release, plain text
	PublishedAt time.Time `json:"publishedAt"`
}

// Status is what the panel and the console show about updates.
type Status struct {
	Current      string    `json:"current"`   // the running version, or dev
	Latest       *Release  `json:"latest"`    // nil until a check succeeded
	Available    bool      `json:"available"` // Latest is newer than Current
	Checking     bool      `json:"checking"`  // the check is switched on
	CheckedAt    time.Time `json:"checkedAt"`
	Error        string    `json:"error,omitempty"` // why the last check failed
	ReleasesURL  string    `json:"releasesUrl"`
	ChangelogURL string    `json:"changelogUrl"`
}

// Version is a parsed release tag such as v1.2.3 or v1.2.3-rc.1.
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

// ParseVersion reads v1.2.3, 1.2.3 and 1.2.3-rc.1. A build metadata suffix is dropped.
func ParseVersion(s string) (Version, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v Version
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, v.Pre = s[:i], s[i+1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("%q is not a version of the form 1.2.3", s)
	}
	nums := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || len(p) > 6 {
			return Version{}, fmt.Errorf("%q is not a version of the form 1.2.3", s)
		}
		nums[i] = n
	}
	v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]
	return v, nil
}

// Compare returns -1, 0 or 1. A release without a pre-release suffix is newer than the
// same numbers with one.
func (v Version) Compare(o Version) int {
	for _, d := range []int{v.Major - o.Major, v.Minor - o.Minor, v.Patch - o.Patch} {
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
	}
	switch {
	case v.Pre == o.Pre:
		return 0
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	case v.Pre < o.Pre:
		return -1
	}
	return 1
}

// Newer reports whether latest is a newer release than current. A current version that is
// not a release (dev, a build from a checkout) is never reported as outdated.
func Newer(current, latest string) bool {
	c, err := ParseVersion(current)
	if err != nil {
		return false
	}
	l, err := ParseVersion(latest)
	if err != nil {
		return false
	}
	return l.Compare(c) > 0
}

type apiRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

// ParseRelease reads the answer of the releases API. It refuses a draft, a pre-release,
// a tag that is not a version and a link outside this repository.
func ParseRelease(data []byte) (Release, error) {
	var r apiRelease
	if err := json.Unmarshal(data, &r); err != nil {
		return Release{}, fmt.Errorf("the release answer is not JSON: %w", err)
	}
	if r.Draft || r.Prerelease {
		return Release{}, errors.New("the latest release is a draft or a pre-release")
	}
	if _, err := ParseVersion(r.TagName); err != nil {
		return Release{}, err
	}
	tag := strings.TrimSpace(r.TagName)
	url := r.HTMLURL
	if !strings.HasPrefix(url, "https://github.com/"+Repo+"/") {
		url = ReleasesURL + "/tag/" + tag
	}
	notes := strings.ToValidUTF8(r.Body, "")
	if len(notes) > maxNotes {
		notes = strings.ToValidUTF8(notes[:maxNotes], "") + "\n…"
	}
	return Release{
		Tag: tag, Version: strings.TrimPrefix(tag, "v"), URL: url,
		Notes: notes, PublishedAt: r.PublishedAt,
	}, nil
}

// Checker asks GitHub for the latest release and remembers the answer.
type Checker struct {
	// Client sends the request. Nil means a client with a timeout and no cookies.
	Client *http.Client
	// URL is the address asked. Empty means LatestURL. Only tests change it.
	URL string
	// Now is the clock. Nil means time.Now.
	Now func() time.Time

	mu        sync.Mutex
	latest    *Release
	checkedAt time.Time
	lastErr   string
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Refresh asks GitHub once and keeps the answer. A failed check keeps the last good answer
// and records the reason.
func (c *Checker) Refresh(ctx context.Context) error {
	rel, err := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkedAt = c.now()
	if err != nil {
		c.lastErr = err.Error()
		return err
	}
	c.latest, c.lastErr = &rel, ""
	return nil
}

func (c *Checker) fetch(ctx context.Context) (Release, error) {
	url := c.URL
	if url == "" {
		url = LatestURL
	}
	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "netscribe-update-check")
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: askTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("ask GitHub for the latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // the answer is read already
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %d to the release check", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return Release{}, fmt.Errorf("read the release answer: %w", err)
	}
	return ParseRelease(data)
}

// Status describes the update state for the running version. It does not ask the network.
func (c *Checker) Status(current string, checking bool) Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := Status{
		Current: current, Checking: checking, CheckedAt: c.checkedAt, Error: c.lastErr,
		ReleasesURL: ReleasesURL, ChangelogURL: ReleasesURL,
	}
	if c.latest != nil {
		rel := *c.latest
		st.Latest = &rel
		st.Available = Newer(current, rel.Tag)
		st.ChangelogURL = rel.URL
	}
	return st
}

// Run checks at start and then every six hours until ctx ends. enabled is asked before every
// check, so a reloaded config switches the check on and off.
func (c *Checker) Run(ctx context.Context, enabled func() bool, logged func(error)) {
	t := time.NewTimer(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if enabled() {
			if err := c.Refresh(ctx); err != nil && logged != nil {
				logged(err)
			}
		}
		t.Reset(checkEvery)
	}
}

// Latest asks GitHub once for the newest release, without keeping the answer.
func Latest(ctx context.Context) (Release, error) { return (&Checker{}).fetch(ctx) }
