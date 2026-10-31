package domain

import "testing"

func TestLineSet(t *testing.T) {
	set := LineSet{Ranges: []LineRange{{First: 3, Last: 5}, {First: 9, Last: 9}}}
	for line, want := range map[int]bool{2: false, 3: true, 5: true, 6: false, 9: true, 10: false} {
		if got := set.Contains(line); got != want {
			t.Errorf("Contains(%d) = %v, want %v", line, got, want)
		}
	}
	if !AllLines().Contains(1000) {
		t.Fatal("AllLines must contain every line")
	}
	if (LineSet{}).Contains(1) {
		t.Fatal("an empty set contains nothing")
	}
}
