package version

import "testing"

func TestShort(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{name: "release", info: Info{Version: "v0.1.0"}, want: "v0.1.0"},
		{name: "dirty", info: Info{Version: "dev", Dirty: true}, want: "dev-dirty"},
		{name: "already marked dirty", info: Info{Version: "v0.1.0-dirty", Dirty: true}, want: "v0.1.0-dirty"},
		{name: "Go dirty metadata", info: Info{Version: "v0.0.0-20260618075507-7a1f010c34be+dirty", Dirty: true}, want: "v0.0.0-20260618075507-7a1f010c34be+dirty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.Short(); got != tt.want {
				t.Fatalf("Short() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	if got, want := resolveVersion(""), "dev"; got != want {
		t.Fatalf("resolveVersion() = %q, want %q", got, want)
	}
}

func TestShortenCommit(t *testing.T) {
	if got, want := shortenCommit("0123456789abcdef"), "0123456789ab"; got != want {
		t.Fatalf("shortenCommit() = %q, want %q", got, want)
	}
}
