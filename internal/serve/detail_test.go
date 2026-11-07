package serve

import "testing"

func TestWebAppSnoozedDetail(t *testing.T) {
	runWebAppTest(t, "detail_test.cjs")
}

func TestWebAppPlainWords(t *testing.T) {
	runWebAppTest(t, "present_test.cjs")
}
