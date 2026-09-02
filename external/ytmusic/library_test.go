package ytmusic

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

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
	if got, _ := b.librarySources(); got != nil {
		t.Errorf("librarySources() = %v, want nil", got)
	}
}

func TestLibraryPlaylistIDsScrapeError(t *testing.T) {
	b := newBase(nil, "client-id", "client-secret", "chrome")
	noNetwork(b)
	b.fetchLibrary = func(string) ([]playlist.PlaylistInfo, error) {
		return nil, errors.New("yt-dlp: boom")
	}
	if got, _ := b.librarySources(); got != nil {
		t.Errorf("librarySources() = %v, want nil on scrape error", got)
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
		if got, _ := b.librarySources(); !slices.Equal(got, []string{"PLytm"}) {
			want := []string{"PLytm"}
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
		if got, _ := b.librarySources(); !slices.Equal(got, []string{"PLfeed"}) {
			want := []string{"PLfeed"}
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
	got, _ := b.librarySources()
	want := []string{"PLboth", "PLfeed", "PLytm"}
	if !slices.Equal(got, want) {
		t.Errorf("librarySources() = %v, want %v", got, want)
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

	got, _ := b.librarySources()
	want := []string{"PLowned", "PLsaved", playlistIDLikedMusic}
	if !slices.Equal(got, want) {
		t.Errorf("librarySources() = %v, want %v", got, want)
	}
	if gotBrowser != "chrome" {
		t.Errorf("browser = %q, want %q", gotBrowser, "chrome")
	}
}

// Playlists in the YouTube Music library are reported as such, so classification
// can mark them music without sampling their video categories.
func TestLibrarySourcesReportsYTMMembership(t *testing.T) {
	b := newBase(nil, "id", "secret", "chrome")
	b.fetchLibrary = func(string) ([]playlist.PlaylistInfo, error) {
		return []playlist.PlaylistInfo{{ID: "PLfeedOnly"}, {ID: "PLboth"}}, nil
	}
	b.fetchYTMLibrary = func(context.Context, string) ([]string, error) {
		return []string{"PLboth", "PLytmOnly"}, nil
	}

	ids, inYTM := b.librarySources()
	if want := []string{"PLfeedOnly", "PLboth", "PLytmOnly"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if inYTM["PLfeedOnly"] {
		t.Error("PLfeedOnly is feed-only, must not be marked as YouTube Music")
	}
	for _, id := range []string{"PLboth", "PLytmOnly"} {
		if !inYTM[id] {
			t.Errorf("%s is in the YouTube Music library, want marked", id)
		}
	}
}

// A YouTube Music playlist is music without any sampling, and the override is
// not written into the on-disk classification cache.
func TestClassifyPlaylistsYTMOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	pls := []playlistEntry{{ID: "PLytm", Name: "In YTM"}}
	inYTM := map[string]bool{"PLytm": true}

	// A nil service would panic if the classifier tried to sample; reaching the
	// end proves the override short-circuits the API entirely.
	got := classifyWithTimeout(nil, pls, time.Second, map[string]bool{}, "scope", inYTM)
	if !got["PLytm"] {
		t.Errorf("classified[PLytm] = false, want true (in YouTube Music library)")
	}
	if disk := loadClassification("scope"); disk["PLytm"] {
		t.Error("YTM override must not be persisted to the classification cache")
	}
}
