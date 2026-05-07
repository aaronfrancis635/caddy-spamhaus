package caddyspamhaus

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
