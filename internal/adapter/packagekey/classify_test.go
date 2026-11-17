package packagekey

import "testing"

func TestMavenPackageFileClassificationUsesRepositoryLayout(t *testing.T) {
	for _, test := range []struct {
		key  string
		want bool
	}{
		{key: "maven/com/acme/app/1.0/app-1.0.war", want: true},
		{key: "maven/com/acme/app/1.0/app-1.0-bin.tar.gz", want: true},
		{key: "maven/com/acme/app/1.0-SNAPSHOT/app-1.0-20260901.010203-7.jar", want: true},
		{key: "maven/com/acme/app/1.0/app-1.0.war.sha256", want: false},
		{key: "maven/com/acme/app/maven-metadata.xml", want: false},
	} {
		if got := IsPackageFile("maven", test.key); got != test.want {
			t.Errorf("IsPackageFile(maven, %q) = %v, want %v", test.key, got, test.want)
		}
	}
}

func TestGoPackageFileClassificationExcludesTheChecksumDatabase(t *testing.T) {
	for _, test := range []struct {
		key  string
		want bool
	}{
		{key: "go/example.com/mod/@v/v1.0.0.zip", want: true},
		{key: "go/example.com/mod/@v/v1.0.0.mod", want: false},
		// A lookup names a version, and a pre-release may be spelled like the
		// name of a module archive.
		{key: "go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-a.zip", want: false},
		{key: "go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0", want: false},
		// Only the exact prefix is the checksum database.
		{key: "go/Sumdb/mod/@v/v1.0.0.zip", want: true},
		{key: "go/sumdb.example.org/mod/@v/v1.0.0.zip", want: true},
	} {
		if got := IsPackageFile("go", test.key); got != test.want {
			t.Errorf("IsPackageFile(go, %q) = %v, want %v", test.key, got, test.want)
		}
	}
}
