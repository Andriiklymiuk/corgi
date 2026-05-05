package utils

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertToRawURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "github blob URL",
			in:   "https://github.com/foo/bar/blob/main/file.txt",
			want: "https://raw.githubusercontent.com/foo/bar/main/file.txt",
		},
		{
			name: "github URL without blob",
			in:   "https://github.com/foo/bar/main/file.txt",
			want: "https://raw.githubusercontent.com/foo/bar/main/file.txt",
		},
		{
			name: "gitlab blob URL",
			in:   "https://gitlab.com/foo/bar/-/blob/main/file.txt",
			want: "https://gitlab.com/foo/bar/-/raw/main/file.txt",
		},
		{
			name: "non-git URL untouched",
			in:   "https://example.com/foo.txt",
			want: "https://example.com/foo.txt",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertToRawURL(tt.in)
			if got != tt.want {
				t.Errorf("convertToRawURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDownloadFileFromURL(t *testing.T) {
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	t.Run("downloads body and writes file", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello-world"))
		}))
		defer srv.Close()

		path, err := DownloadFileFromURL(srv.URL+"/payload.txt", "out.txt", "")
		if err != nil {
			t.Fatalf("download err: %v", err)
		}
		if filepath.Base(path) != "out.txt" {
			t.Errorf("path = %q, want basename out.txt", path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(b) != "hello-world" {
			t.Errorf("body = %q, want hello-world", string(b))
		}
	})

	t.Run("non-200 returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, err := DownloadFileFromURL(srv.URL+"/x.txt", "fail.txt", "")
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Errorf("expected 404 error, got %v", err)
		}
	})

	t.Run("derives filename from URL when empty", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("body"))
		}))
		defer srv.Close()

		path, err := DownloadFileFromURL(srv.URL+"/auto.bin", "", "")
		if err != nil {
			t.Fatalf("download err: %v", err)
		}
		if filepath.Base(path) != "auto.bin" {
			t.Errorf("derived path = %q, want auto.bin", path)
		}
	})
}
