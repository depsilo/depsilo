package config

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRetargetSQLiteDatabasePreservesUnrelatedDocumentBytes(t *testing.T) {
	oldPath := filepath.Join(t.TempDir(), "old", "depsilo.db")
	newPath := filepath.Join(t.TempDir(), "new", "depsilo.db")
	document := []byte("# operator note\r\nconfig_version = 1\r\n\r\n[database]\r\ndriver = 'sqlite'\r\ndsn = " + quoteTOML(oldPath) + " # keep\r\n\r\n[custom]\r\nkeep = { enabled = true }\r\n")

	updated, changed, err := RetargetSQLiteDatabase(document, newPath)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("RetargetSQLiteDatabase reported no change")
	}
	want := strings.Replace(string(document), quoteTOML(oldPath), quoteTOML(newPath), 1)
	if string(updated) != want {
		t.Fatalf("retargeted document:\n%s\nwant:\n%s", updated, want)
	}
}

func TestRetargetSQLiteDatabaseLeavesEquivalentTargetByteForByte(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "depsilo.db")
	document := []byte("[database]\ndriver = \"sqlite\"\ndsn = " + quoteTOML(path) + "\n")

	updated, changed, err := RetargetSQLiteDatabase(document, "file:"+filepath.ToSlash(path)+"?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	if changed || string(updated) != string(document) {
		t.Fatalf("equivalent target changed document:\n%s", updated)
	}
}

func TestRetargetSQLiteDatabaseAddsMissingDSNToDatabaseTable(t *testing.T) {
	target := filepath.Join(t.TempDir(), "recovered.db")
	document := []byte("config_version = 1\n[database]\ndriver = \"sqlite\"\n[custom]\nkeep = true\n")

	updated, changed, err := RetargetSQLiteDatabase(document, target)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || !strings.Contains(string(updated), "driver = \"sqlite\"\ndsn = "+quoteTOML(target)+"\n[custom]") {
		t.Fatalf("missing database.dsn was not inserted in its table:\n%s", updated)
	}
}

func TestRetargetSQLiteDatabaseRejectsInlineTableWithoutDSN(t *testing.T) {
	document := []byte("database = { driver = \"sqlite\" }\n")
	_, _, err := RetargetSQLiteDatabase(document, filepath.Join(t.TempDir(), "recovered.db"))
	if err == nil || !strings.Contains(err.Error(), "inline table") {
		t.Fatalf("error = %v, want actionable inline-table refusal", err)
	}
}

func quoteTOML(value string) string {
	return strconv.Quote(value)
}
