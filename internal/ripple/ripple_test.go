package ripple

import (
	"testing"
)

func TestCharsField(t *testing.T) {
	m := NewWithSeed(1)
	m.SetSize(40, 20)
	for i := 0; i < 30; i++ {
		m.Step()
	}

	field := m.Chars(40, 20)
	if len(field) != 20 {
		t.Fatalf("expected 20 rows, got %d", len(field))
	}

	valid := make(map[rune]bool, len(rippleChars))
	for _, c := range rippleChars {
		valid[c] = true
	}

	nonSpace := 0
	allEqual := true
	for y, row := range field {
		if len(row) != 40 {
			t.Fatalf("row %d: expected 40 columns, got %d", y, len(row))
		}
		for x, c := range row {
			if !valid[c] {
				t.Fatalf("cell (%d,%d): %q is not in the character set", x, y, c)
			}
			if c != ' ' {
				nonSpace++
			}
			if y > 0 || x > 0 {
				if c != field[0][0] {
					allEqual = false
				}
			}
		}
	}
	if nonSpace == 0 {
		t.Fatal("expected at least one non-space cell after 30 steps")
	}
	if allEqual {
		t.Fatal("expected the field to contain more than one distinct character")
	}
}

func TestFreshModel(t *testing.T) {
	m := NewWithSeed(1)
	if !m.Empty() {
		t.Fatal("fresh model should be empty")
	}
	field := m.Chars(40, 20)
	if len(field) != 20 {
		t.Fatalf("expected 20 rows, got %d", len(field))
	}
	for y, row := range field {
		if len(row) != 40 {
			t.Fatalf("row %d: expected 40 columns, got %d", y, len(row))
		}
		for x, c := range row {
			if c != ' ' {
				t.Fatalf("cell (%d,%d): expected space, got %q", x, y, c)
			}
		}
	}
}

func TestDeterminism(t *testing.T) {
	a := NewWithSeed(7)
	a.SetSize(40, 20)
	b := NewWithSeed(7)
	b.SetSize(40, 20)
	for i := 0; i < 30; i++ {
		a.Step()
		b.Step()
	}
	if fa, fb := a.Chars(40, 20), b.Chars(40, 20); !fieldsEqual(fa, fb) {
		t.Fatal("same seed should produce identical fields")
	}

	c := NewWithSeed(8)
	c.SetSize(40, 20)
	for i := 0; i < 30; i++ {
		c.Step()
	}
	if fieldsEqual(a.Chars(40, 20), c.Chars(40, 20)) {
		t.Fatal("different seeds should produce different fields")
	}
}

func TestManySteps(t *testing.T) {
	m := NewWithSeed(42)
	m.SetSize(40, 20)
	for i := 0; i < 3000; i++ {
		m.Step()
	}
	field := m.Chars(40, 20)
	if len(field) != 20 {
		t.Fatalf("expected 20 rows, got %d", len(field))
	}
	for y, row := range field {
		if len(row) != 40 {
			t.Fatalf("row %d: expected 40 columns, got %d", y, len(row))
		}
	}
}

func fieldsEqual(a, b [][]rune) bool {
	if len(a) != len(b) {
		return false
	}
	for y := range a {
		if len(a[y]) != len(b[y]) {
			return false
		}
		for x := range a[y] {
			if a[y][x] != b[y][x] {
				return false
			}
		}
	}
	return true
}
