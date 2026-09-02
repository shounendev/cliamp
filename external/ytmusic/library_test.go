package ytmusic

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/bjarneo/cliamp/playlist"
)

// noNetwork stubs both library sources so tests never shell out or hit the network.
func noNetwork(b *baseProvider) {
	b.fetchLibrary = func(string) ([]playlist.PlaylistInfo, error) { return nil, nil }
	b.fetchYTMLibrary = func(context.Context, string) ([]string, error) { return nil, nil }
}

func TestLibraryPlaylistIDsNoCookies(t *testing.T) {
	b := newBase(nil, "client-id", "client-secret", "")
	b.fetchLibrary = func(string) ([]playlist.PlaylistInfo, error) {
		t.Fatal("fetchLibrary called without a cookie source")
		return nil, nil
	}
	b.fetchYTMLibrary = func(context.Context, string) ([]string, error) {
		t.Fatal("fetchYTMLibrary called without a cookie source")
		return nil, nil
	}
	if got := b.libraryPlaylistIDs(); got != nil {
		t.Errorf("libraryPlaylistIDs() = %v, want nil", got)
	}
}

func TestLibraryPlaylistIDsScrapeError(t *testing.T) {
	b := newBase(nil, "client-id", "client-secret", "chrome")
	noNetwork(b)
	b.fetchLibrary = func(string) ([]playlist.PlaylistInfo, error) {
		return nil, errors.New("yt-dlp: boom")
	}
	if got := b.libraryPlaylistIDs(); got != nil {
		t.Errorf("libraryPlaylistIDs() = %v, want nil on scrape error", got)
	}
}

// A failure of one source must not discard the other's results.
func TestLibraryPlaylistIDsSourcesAreIndependent(t *testing.T) {
	t.Run("feed fails, ytm succeeds", func(t *testing.T) {
		b := newBase(nil, "id", "secret", "chrome")
		b.fetchLibrary = func(string) ([]playlist.PlaylistInfo, error) {
			return nil, errors.New("yt-dlp: boom")
		}
		b.fetchYTMLibrary = func(context.Context, string) ([]string, error) {
			return []string{"PLytm"}, nil
		}
		if got, want := b.libraryPlaylistIDs(), []string{"PLytm"}; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("ytm fails, feed succeeds", func(t *testing.T) {
		b := newBase(nil, "id", "secret", "chrome")
		b.fetchLibrary = func(string) ([]playlist.PlaylistInfo, error) {
			return []playlist.PlaylistInfo{{ID: "PLfeed"}}, nil
		}
		b.fetchYTMLibrary = func(context.Context, string) ([]string, error) {
			return nil, errors.New("innertube: HTTP 401")
		}
		if got, want := b.libraryPlaylistIDs(), []string{"PLfeed"}; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// Playlists present in both the feed and the YouTube Music library must appear once.
func TestLibraryPlaylistIDsDedupesAcrossSources(t *testing.T) {
	b := newBase(nil, "id", "secret", "chrome")
	b.fetchLibrary = func(string) ([]playlist.PlaylistInfo, error) {
		return []playlist.PlaylistInfo{{ID: "PLboth"}, {ID: "PLfeed"}}, nil
	}
	b.fetchYTMLibrary = func(context.Context, string) ([]string, error) {
		return []string{"PLboth", "PLytm"}, nil
	}
	got := b.libraryPlaylistIDs()
	want := []string{"PLboth", "PLfeed", "PLytm"}
	if !slices.Equal(got, want) {
		t.Errorf("libraryPlaylistIDs() = %v, want %v", got, want)
	}
}

func TestLibraryPlaylistIDs(t *testing.T) {
	var gotBrowser string
	b := newBase(nil, "client-id", "client-secret", " chrome ")
	noNetwork(b)
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
