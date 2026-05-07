package caddyspamhaus

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	caddyhttp "github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(&SpamhausDROP{})
	caddy.RegisterModule(&SpamhausDropMatcher{})
}

const (
	defaultURL      = "https://www.spamhaus.org/drop/drop_v4.json"
	defaultInterval = 24 * time.Hour
)

type spamhausEntry struct {
	CIDR  string `json:"cidr"`
	SBLID string `json:"sblid"`
	RIR   string `json:"rir"`
}

type SpamhausDROP struct {
	URL string `json:"url,omitempty"`

	RefreshInterval caddy.Duration `json:"refresh_interval,omitempty"`

	logger *zap.Logger

	mu        sync.RWMutex
	ranges    []netip.Prefix
	lastFetch time.Time

	cancel context.CancelFunc
}

func (*SpamhausDROP) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.ip_sources.spamhaus_drop",
		New: func() caddy.Module { return new(SpamhausDROP) },
	}
}

func (s *SpamhausDROP) Provision(ctx caddy.Context) error {
	s.logger = ctx.Logger()

	if s.URL == "" {
		s.URL = defaultURL
	}
	if s.RefreshInterval == 0 {
		s.RefreshInterval = caddy.Duration(defaultInterval)
	}

	if err := s.fetchAndStore(); err != nil {
		return fmt.Errorf("spamhaus_drop: initial fetch from %s failed: %w", s.URL, err)
	}

	bgCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go s.refreshLoop(bgCtx)

	return nil
}

func (s *SpamhausDROP) Cleanup() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *SpamhausDROP) GetIPRanges(_ *http.Request) []netip.Prefix {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ranges
}

func (s *SpamhausDROP) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(s.RefreshInterval))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.fetchAndStore(); err != nil {
				s.logger.Error("spamhaus_drop: failed to refresh list; continuing with cached data",
					zap.String("url", s.URL),
					zap.Error(err),
				)
			}
		}
	}
}

func (s *SpamhausDROP) fetchAndStore() error {
	prefixes, err := fetchDropList(s.URL)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.ranges = prefixes
	s.lastFetch = time.Now()
	s.mu.Unlock()

	if s.logger != nil {
		s.logger.Info("spamhaus_drop: list refreshed",
			zap.String("url", s.URL),
			zap.Int("prefixes", len(prefixes)),
		)
	}
	return nil
}

// {"cidr":"1.10.16.0/20","sblid":"SBL256894","rir":"apnic"}
func fetchDropList(url string) ([]netip.Prefix, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	var prefixes []netip.Prefix
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry spamhausEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.CIDR == "" {
			continue
		}

		prefix, err := netip.ParsePrefix(entry.CIDR)
		if err != nil {
			continue
		}
		prefixes = append(prefixes, prefix)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading response body from %s: %w", url, err)
	}

	return prefixes, nil
}

// Syntax:
//
//	spamhaus_drop [<url>] {
//	    url             <url>
//	    refresh_interval <duration>
//	}
func (s *SpamhausDROP) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()

	if d.NextArg() {
		s.URL = d.Val()
	}

	for d.NextBlock(0) {
		switch d.Val() {
		case "url":
			if !d.NextArg() {
				return d.ArgErr()
			}
			s.URL = d.Val()

		case "refresh_interval":
			if !d.NextArg() {
				return d.ArgErr()
			}
			dur, err := time.ParseDuration(d.Val())
			if err != nil {
				return d.Errf("invalid refresh_interval %q: %v", d.Val(), err)
			}
			s.RefreshInterval = caddy.Duration(dur)

		default:
			return d.Errf("unknown subdirective %q", d.Val())
		}
	}
	return nil
}

var (
	_ caddy.Provisioner       = (*SpamhausDROP)(nil)
	_ caddy.CleanerUpper      = (*SpamhausDROP)(nil)
	_ caddyfile.Unmarshaler   = (*SpamhausDROP)(nil)
	_ caddyhttp.IPRangeSource = (*SpamhausDROP)(nil)
)

//	@blocked {
//	    spamhaus_drop
//	}
//
// abort @blocked
type SpamhausDropMatcher struct {
	URL string `json:"url,omitempty"`

	RefreshInterval caddy.Duration `json:"refresh_interval,omitempty"`

	logger *zap.Logger
	mu     sync.RWMutex
	ranges []netip.Prefix
	cancel context.CancelFunc
}

func (*SpamhausDropMatcher) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.matchers.spamhaus_drop",
		New: func() caddy.Module { return new(SpamhausDropMatcher) },
	}
}

func (m *SpamhausDropMatcher) Provision(ctx caddy.Context) error {
	m.logger = ctx.Logger()
	if m.URL == "" {
		m.URL = defaultURL
	}
	if m.RefreshInterval == 0 {
		m.RefreshInterval = caddy.Duration(defaultInterval)
	}

	if err := m.fetchAndStore(); err != nil {
		return fmt.Errorf("spamhaus_drop matcher: initial fetch from %s failed: %w", m.URL, err)
	}

	bgCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	go m.refreshLoop(bgCtx)
	return nil
}

func (m *SpamhausDropMatcher) Cleanup() error {
	if m.cancel != nil {
		m.cancel()
	}
	return nil
}

func (m *SpamhausDropMatcher) Match(r *http.Request) bool {
	host, err := parseRemoteAddr(r.RemoteAddr)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, prefix := range m.ranges {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (m *SpamhausDropMatcher) MatchWithError(r *http.Request) (bool, error) {
	return m.Match(r), nil
}

func (m *SpamhausDropMatcher) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(m.RefreshInterval))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.fetchAndStore(); err != nil {
				m.logger.Error("spamhaus_drop matcher: refresh failed; using cached list",
					zap.String("url", m.URL),
					zap.Error(err),
				)
			}
		}
	}
}

func (m *SpamhausDropMatcher) fetchAndStore() error {
	prefixes, err := fetchDropList(m.URL)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.ranges = prefixes
	m.mu.Unlock()
	if m.logger != nil {
		m.logger.Info("spamhaus_drop matcher: list refreshed",
			zap.String("url", m.URL),
			zap.Int("prefixes", len(prefixes)),
		)
	}
	return nil
}

func (m *SpamhausDropMatcher) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	if d.NextArg() {
		m.URL = d.Val()
	}
	for d.NextBlock(0) {
		switch d.Val() {
		case "url":
			if !d.NextArg() {
				return d.ArgErr()
			}
			m.URL = d.Val()
		case "refresh_interval":
			if !d.NextArg() {
				return d.ArgErr()
			}
			dur, err := time.ParseDuration(d.Val())
			if err != nil {
				return d.Errf("invalid refresh_interval %q: %v", d.Val(), err)
			}
			m.RefreshInterval = caddy.Duration(dur)
		default:
			return d.Errf("unknown subdirective %q", d.Val())
		}
	}
	return nil
}

func parseRemoteAddr(remote string) (host string, err error) {
	if h, _, err := net.SplitHostPort(remote); err == nil {
		return h, nil
	}
	return remote, nil
}

var (
	_ caddy.Provisioner                 = (*SpamhausDropMatcher)(nil)
	_ caddy.CleanerUpper                = (*SpamhausDropMatcher)(nil)
	_ caddyfile.Unmarshaler             = (*SpamhausDropMatcher)(nil)
	_ caddyhttp.RequestMatcherWithError = (*SpamhausDropMatcher)(nil)
)
