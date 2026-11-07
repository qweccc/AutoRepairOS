package db

import "testing"

func TestSplitStatements(t *testing.T) {
	input := `CREATE TABLE t (id INTEGER);
-- comment
INSERT INTO t VALUES (1);

SELECT * FROM t;`
	got := splitStatements(input)
	if len(got) != 3 {
		t.Errorf("expected 3, got %d: %v", len(got), got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("got %s", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("got %s", got)
	}
}
