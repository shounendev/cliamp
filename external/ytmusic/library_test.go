package ytmusic

import (
	"errors"
	"slices"
	"testing"

	"github.com/bjarneo/cliamp/playlist"
)

func TestLibraryPlaylistIDsNoCookies(t *testing.T) {
	b := newBase(nil, "client-id", "client-secret", "")
	b.fetchLibrary = func(string) ([]playlist.PlaylistInfo, error) {
		t.Fatal("fetchLibrary called without a cookie source")
		return nil, nil
	}
	if got := b.libraryPlaylistIDs(); got != nil {
		t.Errorf("libraryPlaylistIDs() = %v, want nil", got)
	}
}

func TestLibraryPlaylistIDsScrapeError(t *testing.T) {
	b := newBase(nil, "client-id", "client-secret", "chrome")
	b.fetchLibrary = func(string) ([]playlist.PlaylistInfo, error) {
		return nil, errors.New("yt-dlp: boom")
	}
	if got := b.libraryPlaylistIDs(); got != nil {
		t.Errorf("libraryPlaylistIDs() = %v, want nil on scrape error", got)
	}
}

func TestLibraryPlaylistIDs(t *testing.T) {
	var gotBrowser string
	b := newBase(nil, "client-id", "client-secret", " chrome ")
	b.fetchLibrary = func(browser string) ([]playlist.PlaylistInfo, error) {
		gotBrowser = browser
		return []playlist.PlaylistInfo{
			{ID: "PLowned", Name: "Mine"},
			{ID: " PLsaved ", Name: "Saved from another channel"},
			{ID: "", Name: "junk"},
			{ID: playlistIDLikedMusic, Name: "Liked Music"},
		}, nil
	}

	got := b.libraryPlaylistIDs()
	want := []string{"PLowned", "PLsaved", playlistIDLikedMusic}
	if !slices.Equal(got, want) {
		t.Errorf("libraryPlaylistIDs() = %v, want %v", got, want)
	}
	if gotBrowser != "chrome" {
		t.Errorf("browser = %q, want %q", gotBrowser, "chrome")
	}
}
