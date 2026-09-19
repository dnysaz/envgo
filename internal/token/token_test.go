package token

import "testing"

func TestNew(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 {
		t.Fatalf("token length = %d, want 64", len(a))
	}
	if a == b {
		t.Fatal("two tokens must differ")
	}
}
