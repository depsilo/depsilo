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
