package traffic_feature

import "testing"

func TestTileModelRelativePath(t *testing.T) {
	tests := []struct {
		geohash string
		want    string
	}{
		{geohash: "wtw", want: "tiles/w/wtw.glb"},
		{geohash: "wtw3", want: "tiles/wt/wtw3.glb"},
		{geohash: "wtw3s", want: "tiles/wt/wtw/wtw3s.glb"},
		{geohash: "wtw3sjq", want: "tiles/wt/wtw3/wtw3s/wtw3sjq.glb"},
		{geohash: "wtw3sjq9", want: "tiles/wt/wtw3/wtw3sj/wtw3sjq9.glb"},
	}

	for _, tt := range tests {
		t.Run(tt.geohash, func(t *testing.T) {
			if got := tileModelRelativePath(tt.geohash); got != tt.want {
				t.Fatalf("tileModelRelativePath(%q) = %q, want %q", tt.geohash, got, tt.want)
			}
		})
	}
}
