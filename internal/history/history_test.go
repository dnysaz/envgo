package history

import "testing"

func TestRingBuffer(t *testing.T) {
	h := New(2)
	h.Add(Entry{Host: "a", Status: 200})
	h.Add(Entry{Host: "b", Status: 201})
	h.Add(Entry{Host: "c", Status: 202})

	list := h.List()
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	if list[0].Host != "c" || list[1].Host != "b" {
		t.Fatalf("newest-first order wrong: %+v", list)
	}
}

func TestDefaultMax(t *testing.T) {
	h := New(0)
	if h.max != 200 {
		t.Fatalf("max = %d, want 200", h.max)
	}
}
