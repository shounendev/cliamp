package ytmusic

import (
	"strings"
	"testing"
)

func TestSAPISIDHash(t *testing.T) {
	// SHA-1 over "1700000000 SECRET https://music.youtube.com".
	got := sapisidHash("SECRET", "https://music.youtube.com", 1700000000)
	// Cross-checked against an independent SHA-1 of the same input string.
	const want = "SAPISIDHASH 1700000000_b775e3600857ebf92a806a130f297aaa9ddcc3f4"
	if !strings.HasPrefix(got, "SAPISIDHASH 1700000000_") {
		t.Fatalf("sapisidHash() = %q, want SAPISIDHASH <ts>_<hex>", got)
	}
	if got != want {
		t.Errorf("sapisidHash() = %q, want %q", got, want)
	}
}

func TestParseLibraryPlaylists(t *testing.T) {
	body := []byte(`{"contents":{"x":[
		{"musicTwoRowItemRenderer":{"title":{"runs":[{"text":"Saved"}]},
		 "navigationEndpoint":{"browseEndpoint":{"browseId":"VLPLsaved"}}}},
		{"musicTwoRowItemRenderer":{"title":{"runs":[{"text":"Liked"}]},
		 "navigationEndpoint":{"browseEndpoint":{"browseId":"VLLM"}}}},
		{"musicTwoRowItemRenderer":{"title":{"runs":[{"text":"An artist"}]},
		 "navigationEndpoint":{"browseEndpoint":{"browseId":"UCchannel"}}}},
		{"musicTwoRowItemRenderer":{"title":{"runs":[{"text":"No endpoint"}]}}},
		{"continuationItemRenderer":{"continuationEndpoint":{
		 "continuationCommand":{"token":"NEXTPAGE"}}}}]}}`)

	ids, cont := parseLibraryPlaylists(body)
	want := []string{"PLsaved", "LM"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
	if cont != "NEXTPAGE" {
		t.Errorf("continuation = %q, want %q", cont, "NEXTPAGE")
	}
}

func TestParseLibraryPlaylistsMalformed(t *testing.T) {
	ids, cont := parseLibraryPlaylists([]byte(`not json`))
	if ids != nil || cont != "" {
		t.Errorf("parseLibraryPlaylists(garbage) = %v, %q; want nil, \"\"", ids, cont)
	}
}

func TestParseNetscapeCookies(t *testing.T) {
	jar := strings.Join([]string{
		"# Netscape HTTP Cookie File",
		".youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tsecret-value",
		".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tsid-value",
		".example.com\tTRUE\t/\tTRUE\t0\tOTHER\tignored",
		"malformed line",
	}, "\n")

	header, sapisid, err := parseNetscapeCookies(jar)
	if err != nil {
		t.Fatalf("parseNetscapeCookies() error = %v", err)
	}
	if sapisid != "secret-value" {
		t.Errorf("sapisid = %q, want %q", sapisid, "secret-value")
	}
	if !strings.Contains(header, "SAPISID=secret-value") || !strings.Contains(header, "SID=sid-value") {
		t.Errorf("header = %q, missing youtube cookies", header)
	}
	if strings.Contains(header, "OTHER") {
		t.Errorf("header = %q, should not carry non-youtube cookies", header)
	}
}

func TestParseNetscapeCookiesNoYouTube(t *testing.T) {
	if _, _, err := parseNetscapeCookies(".example.com\tTRUE\t/\tTRUE\t0\tA\tb"); err == nil {
		t.Error("parseNetscapeCookies() with no youtube cookies: want error, got nil")
	}
}

// __Secure-3PAPISID stands in when SAPISID is absent.
func TestParseNetscapeCookiesFallbackSAPISID(t *testing.T) {
	jar := ".youtube.com\tTRUE\t/\tTRUE\t0\t__Secure-3PAPISID\tfallback"
	_, sapisid, err := parseNetscapeCookies(jar)
	if err != nil {
		t.Fatalf("parseNetscapeCookies() error = %v", err)
	}
	if sapisid != "fallback" {
		t.Errorf("sapisid = %q, want %q", sapisid, "fallback")
	}
}

// Album entries navigate to an MPREb_ browse ID the Data API cannot serve, so
// the album's own OLAK5uy_ track playlist must be picked up instead — and not
// the RDAMPL… radio playlist sitting beside it.
func TestParseLibraryPlaylistsAlbums(t *testing.T) {
	body := []byte(`{"contents":[
		{"musicTwoRowItemRenderer":{
		  "title":{"runs":[{"text":"R Plus Seven"}]},
		  "navigationEndpoint":{"browseEndpoint":{"browseId":"MPREb_eZhiASkN4bg"}},
		  "menu":{"items":[{"menuNavigationItemRenderer":{"navigationEndpoint":
		    {"watchEndpoint":{"playlistId":"RDAMPLOLAK5uy_radio"}}}}]},
		  "thumbnailOverlay":{"musicItemThumbnailOverlayRenderer":{"content":
		    {"musicPlayButtonRenderer":{"playNavigationEndpoint":
		      {"watchPlaylistEndpoint":{"playlistId":"OLAK5uy_album1"}}}}}}}},
		{"musicTwoRowItemRenderer":{
		  "title":{"runs":[{"text":"An artist"}]},
		  "navigationEndpoint":{"browseEndpoint":{"browseId":"UCsomechannel"}}}}]}`)

	ids, _ := parseLibraryPlaylists(body)
	if len(ids) != 1 {
		t.Fatalf("ids = %v, want exactly the album playlist", ids)
	}
	if ids[0] != "OLAK5uy_album1" {
		t.Errorf("ids[0] = %q, want %q", ids[0], "OLAK5uy_album1")
	}
}

// A playlist entry must still resolve by its VL browse ID, not by scanning for
// an album playlist that is not there.
func TestItemPlaylistIDPrefersBrowseID(t *testing.T) {
	item := map[string]any{
		"navigationEndpoint": map[string]any{
			"browseEndpoint": map[string]any{"browseId": "VLPLreal"},
		},
		"menu": map[string]any{"playlistId": "OLAK5uy_shouldNotWin"},
	}
	if got := itemPlaylistID(item); got != "PLreal" {
		t.Errorf("itemPlaylistID() = %q, want %q", got, "PLreal")
	}
}
