package signal

import (
	"strings"
	"testing"
)

func TestReadPathsSkipsBlanks(t *testing.T) {
	got := ReadPaths(strings.NewReader("/a\n\n  \n/b c\n"))
	if len(got) != 2 || got[0] != "/a" || got[1] != "/b c" {
		t.Fatalf("%v", got)
	}
}

func TestReadLinesRejectsBadLine(t *testing.T) {
	if _, err := readLines(strings.NewReader("{\"v\":0}\nnope\n")); err == nil {
		t.Fatal("strict reader must error on a bad line")
	}
}

func TestMigratorsRefuseUnknown(t *testing.T) {
	if _, err := (ObsMigrator{}).Migrate(0, nil); err == nil {
		t.Fatal("ObsMigrator must refuse")
	}
}
