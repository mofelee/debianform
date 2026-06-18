package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConfigFilesLoadsAllDBFHCLInCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	for _, name := range []string{"20-app.dbf.hcl", "10-base.dbf.hcl", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := configFiles("")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10-base.dbf.hcl", "20-app.dbf.hcl"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("configFiles() = %#v, want %#v", files, want)
	}
}

func TestConfigFilesWithExplicitFile(t *testing.T) {
	files, err := configFiles("custom.dbf.hcl")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"custom.dbf.hcl"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("configFiles() = %#v, want %#v", files, want)
	}
}
