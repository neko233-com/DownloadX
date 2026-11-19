package desktop

import "testing"

func TestLooksDownloadable(t *testing.T) {
	yes := []string{
		"https://example.com/file.zip",
		"https://example.com/file.MSI?token=1",
		"ftp://example.com/file.tar.gz",
		"https://example.com/video.m3u8",
		"https://example.com/manifest.mpd",
		"https://example.com/movie.torrent",
		"magnet:?xt=urn:btih:0123456789abcdef",
	}
	for _, s := range yes {
		if !looksDownloadable(s) {
			t.Errorf("looksDownloadable(%q) = false, want true", s)
		}
	}
	no := []string{
		"https://example.com/",
		"https://example.com/article.html",
		"https://example.com/news/story",
		"https://example.com/file.zip/",
		"https://example.com/?file=test.zip",
		"https://example.com/file.xyz",
		"not a url",
	}
	for _, s := range no {
		if looksDownloadable(s) {
			t.Errorf("looksDownloadable(%q) = true, want false", s)
		}
	}
}
