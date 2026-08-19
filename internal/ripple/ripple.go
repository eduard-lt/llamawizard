// Package ripple implements the animated character "rain" background:
// expanding character rings over a dark field, rendered at 15 fps.
package ripple

import (
	"math"
	"math/rand"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// drop is a single expanding ring in the rain field.
type drop struct {
	x, y float64
	r    float64
	maxR float64
	life float64
}

// Model holds the state of the ripple rain animation.
type Model struct {
	drops    []drop
	rng      *rand.Rand
	width    int
	height   int
}

var rippleChars = []rune{' ', '.', ':', '-', '=', '+', '*', '#', '%', '@'}

// New returns a Model seeded from the current time.
func New() Model {
	return NewWithSeed(time.Now().UnixNano())
}

// NewWithSeed returns a Model with a deterministic random source, for tests.
func NewWithSeed(seed int64) Model {
	return Model{rng: rand.New(rand.NewSource(seed))}
}

// SetSize sets the animation field size in cells.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Empty reports whether there are no live drops.
func (m *Model) Empty() bool {
	return len(m.drops) == 0
}

// TickMsg is sent by Tick to advance the animation one frame.
type TickMsg struct{}

// Tick returns a cmd that sends TickMsg every 1/15 second.
func (m *Model) Tick() tea.Cmd {
	return tea.Tick(time.Second/15, func(t time.Time) tea.Msg {
		return TickMsg{}
	})
}

// Step advances the animation one frame.
func (m *Model) Step() {
	if m.width == 0 || m.height == 0 {
		return
	}
	rate := 0.15 + m.rng.Float64()*0.15

	var alive []drop
	for _, d := range m.drops {
		d.r += 0.25
		d.life -= 0.015
		if d.life > 0 && d.r < d.maxR {
			alive = append(alive, d)
		}
	}

	if m.rng.Float64() < rate {
		x := 4 + m.rng.Float64()*float64(m.width-8)
		y := 2 + m.rng.Float64()*float64(m.height-4)
		maxR := 4 + m.rng.Float64()*14
		alive = append(alive, drop{x: x, y: y, r: 0.3, maxR: maxR, life: 1.0})
	}

	m.drops = alive
}

// Chars returns the w×h character field, with each cell holding the
// character to render at that grid position.
func (m *Model) Chars(w, h int) [][]rune {
	field := make([][]rune, h)
	for y := 0; y < h; y++ {
		row := make([]rune, w)
		for x := 0; x < w; x++ {
			row[x] = m.charAt(x, y)
		}
		field[y] = row
	}
	return field
}

func (m *Model) charAt(gx, gy int) rune {
	if len(m.drops) == 0 {
		return ' '
	}
	best := float64(0.0)
	for _, d := range m.drops {
		ri := int(d.r)
		if ri <= 0 {
			continue
		}
		dist := math.Sqrt(float64((gx-int(d.x))*(gx-int(d.x))+(gy-int(d.y))*(gy-int(d.y)))) + 0.3
		outerR := float64(ri) + 0.6
		innerR := float64(ri) - 0.8
		if innerR < 0 {
			innerR = 0
		}
		if dist >= innerR && dist <= outerR {
			edge := math.Abs(dist - float64(ri))
			sharp := 1.0 - (edge / 0.8)
			if sharp > 0 {
				v := d.life * sharp
				if v > best {
					best = v
				}
			}
		}
	}
	if best <= 0 {
		return ' '
	}
	ci := int(best * float64(len(rippleChars)-1))
	if ci >= len(rippleChars) {
		ci = len(rippleChars) - 1
	}
	return rippleChars[ci]
}
