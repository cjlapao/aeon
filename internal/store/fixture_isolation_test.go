package store

import "testing"

func TestClonedStoresKeepRowsAndFilesIndependent(t *testing.T) {
	first, second := openTemp(t), openTemp(t)
	if first.Path() == second.Path() {
		t.Fatal("two fixtures share a database file")
	}
	seedLoop(t, first, "only-first", "/repo")
	var count int
	if err := second.r.QueryRow("SELECT count(*) FROM loop").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("fixture inherited another test's rows: %d", count)
	}
	third := openTemp(t)
	if err := third.r.QueryRow("SELECT count(*) FROM loop").Scan(&count); err != nil || count != 0 {
		t.Fatalf("fixture template was changed by its first caller: count=%d, err=%v", count, err)
	}
}
