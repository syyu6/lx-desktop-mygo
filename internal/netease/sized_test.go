package netease

import "testing"

// Sized asks NetEase's CDN for a picture at a size, and leaves another
// source's address alone: it resizes itself, or its query is its own.
func TestSized(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"http://p1.music.126.net/x.jpg", "https://p1.music.126.net/x.jpg?param=96y96"},
		{"http://p1.music.126.net/x.jpg?param=300y300", "https://p1.music.126.net/x.jpg?param=96y96"},
		{"https://y.gtimg.cn/music/photo_new/x.jpg", "https://y.gtimg.cn/music/photo_new/x.jpg?param=96y96"},
		{"http://artistpicserver.kuwo.cn/pic.web?pictype=300&rid=1", "http://artistpicserver.kuwo.cn/pic.web?pictype=300&rid=1"},
	} {
		if got := Sized(tc.in, 96); got != tc.want {
			t.Errorf("Sized(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
