package ytmusic

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// YouTube Music keeps a library separate from youtube.com/feed/playlists — a
// playlist saved inside YouTube Music does not necessarily surface in the
// YouTube feed. yt-dlp refuses music.youtube.com library URLs outright
// ("YouTube Music is not directly supported"), and the Data API has no notion
// of the YouTube Music library at all, so the listing comes from the same
// InnerTube endpoint the YouTube Music web client calls.

const (
	innertubeBrowseURL = "https://music.youtube.com/youtubei/v1/browse"
	innertubeOrigin    = "https://music.youtube.com"

	// albumPlaylistPrefix marks the auto-generated playlist that holds an
	// album's tracks. The Data API serves these like any other playlist.
	albumPlaylistPrefix = "OLAK5uy_"

	innertubeClientName    = "WEB_REMIX"
	innertubeClientVersion = "1.20250101.01.00"

	// innertubeMaxPages bounds continuation paging so a malformed or looping
	// response cannot spin forever.
	innertubeMaxPages = 20

	innertubeMaxBody = 32 << 20
)

var innertubeClient = &http.Client{Timeout: 30 * time.Second}

// innertubeLibraryBrowseIDs are the YouTube Music library shelves that hold
// playable collections. Albums live in their own shelf and are missed entirely
// if only the playlists shelf is read.
var innertubeLibraryBrowseIDs = []string{
	"FEmusic_liked_playlists", // Library › Playlists
	"FEmusic_liked_albums",    // Library › Albums
}

// ytmLibraryPlaylistIDs returns the playlist IDs in the user's YouTube Music
// library, including playlists saved from other channels. browser is a yt-dlp
// cookie specifier (e.g. "chrome" or "chrome+gnomekeyring").
func ytmLibraryPlaylistIDs(ctx context.Context, browser string) ([]string, error) {
	cookieHeader, sapisid, err := browserCookies(ctx, browser)
	if err != nil {
		return nil, err
	}
	if sapisid == "" {
		return nil, fmt.Errorf("no SAPISID cookie — not signed in to YouTube in %s", browser)
	}

	var (
		ids      []string
		seen     = make(map[string]bool)
		firstErr error
	)
	for _, browseID := range innertubeLibraryBrowseIDs {
		shelfIDs, err := browseLibraryShelf(ctx, cookieHeader, sapisid, browseID)
		if err != nil {
			// One empty or failing shelf must not lose the other's contents.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, id := range shelfIDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return ids, nil
}

// browseLibraryShelf pages through one library shelf, returning its playlist IDs.
func browseLibraryShelf(ctx context.Context, cookieHeader, sapisid, browseID string) ([]string, error) {
	var (
		ids          []string
		continuation string
	)
	for page := 0; page < innertubeMaxPages; page++ {
		body, err := innertubeBrowse(ctx, cookieHeader, sapisid, browseID, continuation)
		if err != nil {
			if page > 0 {
				break // keep whatever paged in successfully
			}
			return nil, err
		}
		pageIDs, next := parseLibraryPlaylists(body)
		ids = append(ids, pageIDs...)
		if next == "" || next == continuation {
			break
		}
		continuation = next
	}
	return ids, nil
}

// innertubeBrowse issues one browse request. An empty continuation requests the
// first page; otherwise the token is followed.
func innertubeBrowse(ctx context.Context, cookieHeader, sapisid, browseID, continuation string) ([]byte, error) {
	payload := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    innertubeClientName,
				"clientVersion": innertubeClientVersion,
				"hl":            "en",
				"gl":            "US",
			},
		},
	}
	if continuation == "" {
		payload["browseId"] = browseID
	} else {
		payload["continuation"] = continuation
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, innertubeBrowseURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	ts := time.Now().Unix()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", innertubeOrigin)
	req.Header.Set("Referer", innertubeOrigin+"/")
	req.Header.Set("X-Origin", innertubeOrigin)
	req.Header.Set("X-Goog-AuthUser", "0")
	req.Header.Set("Authorization", sapisidHash(sapisid, innertubeOrigin, ts))
	req.Header.Set("Cookie", cookieHeader)

	resp, err := innertubeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("innertube browse: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("innertube browse: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, innertubeMaxBody))
}

// sapisidHash builds the SAPISIDHASH Authorization header that YouTube's web
// clients send: SHA-1 over "<unix ts> <SAPISID> <origin>".
func sapisidHash(sapisid, origin string, ts int64) string {
	sum := sha1.Sum(fmt.Appendf(nil, "%d %s %s", ts, sapisid, origin))
	return fmt.Sprintf("SAPISIDHASH %d_%x", ts, sum)
}

// parseLibraryPlaylists pulls playlist IDs and the next continuation token out
// of a browse response. InnerTube responses are deeply nested and their shape
// shifts without notice, so this walks the decoded JSON for the two keys it
// needs rather than modelling the whole payload.
func parseLibraryPlaylists(body []byte) (ids []string, continuation string) {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, ""
	}

	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if item, ok := v["musicTwoRowItemRenderer"].(map[string]any); ok {
				if id := itemPlaylistID(item); id != "" {
					ids = append(ids, id)
				}
			}
			if cmd, ok := v["continuationCommand"].(map[string]any); ok {
				if tok, ok := cmd["token"].(string); ok && tok != "" {
					continuation = tok
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc)
	return ids, continuation
}

// itemPlaylistID extracts the playable playlist ID from a library item.
// Playlist entries navigate to a "VL"-prefixed ("View List") browse ID. Album
// entries navigate to an album browse ID (MPREb_…) that the Data API does not
// serve, but they carry their track playlist ("OLAK5uy_…") alongside, which it
// does. Other item types (artists, podcasts) yield neither and are skipped.
func itemPlaylistID(item map[string]any) string {
	if nav, ok := item["navigationEndpoint"].(map[string]any); ok {
		if be, ok := nav["browseEndpoint"].(map[string]any); ok {
			if id, _ := be["browseId"].(string); strings.HasPrefix(id, "VL") && len(id) > 2 {
				return strings.TrimPrefix(id, "VL")
			}
		}
	}
	return albumPlaylistID(item)
}

// albumPlaylistID finds an album's track playlist within its item renderer.
// The same item also carries a "RDAMPL…"-prefixed radio playlist built from the
// album; only the bare OLAK5uy_ ID holds the album's own tracks.
func albumPlaylistID(node any) string {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "playlistId" || key == "audioPlaylistId" {
				if id, ok := child.(string); ok && strings.HasPrefix(id, albumPlaylistPrefix) {
					return id
				}
			}
			if id := albumPlaylistID(child); id != "" {
				return id
			}
		}
	case []any:
		for _, child := range v {
			if id := albumPlaylistID(child); id != "" {
				return id
			}
		}
	}
	return ""
}

// browserCookies exports the browser's cookie jar via yt-dlp and returns a
// Cookie header for youtube.com plus the SAPISID used to sign requests.
// Passing no URL makes yt-dlp write the jar and exit without any network I/O.
func browserCookies(ctx context.Context, browser string) (cookieHeader, sapisid string, err error) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return "", "", fmt.Errorf("yt-dlp not found in PATH")
	}
	dir, err := os.MkdirTemp("", "cliamp-cookies-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "cookies.txt")

	cmd := exec.CommandContext(ctx, "yt-dlp", "--cookies-from-browser", browser, "--cookies", path)
	cmd.WaitDelay = 3 * time.Second
	// yt-dlp exits non-zero because no URL was given; the jar is still written.
	_ = cmd.Run()

	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("yt-dlp wrote no cookie jar for %q", browser)
	}
	return parseNetscapeCookies(string(data))
}

// parseNetscapeCookies reads a Netscape-format cookie jar, keeping YouTube
// cookies. Fields are: domain, flag, path, secure, expiry, name, value.
func parseNetscapeCookies(jar string) (cookieHeader, sapisid string, err error) {
	var pairs []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(jar, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 7 {
			continue
		}
		domain, name, value := f[0], f[5], f[6]
		if !strings.HasSuffix(domain, "youtube.com") || seen[name] {
			continue
		}
		seen[name] = true
		pairs = append(pairs, name+"="+value)
		if name == "SAPISID" || (sapisid == "" && name == "__Secure-3PAPISID") {
			sapisid = value
		}
	}
	if len(pairs) == 0 {
		return "", "", fmt.Errorf("no youtube.com cookies in jar")
	}
	return strings.Join(pairs, "; "), sapisid, nil
}
