package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCanvasDotPlacement(t *testing.T) {
	c := NewCanvas(2, 2) // 4x8 pixels
	c.SetDot(0, 0, 0)
	rows := c.Render()
	if !strings.Contains(rows[0], "⠁") {
		t.Errorf("expected top-left dot ⠁ in row 0, got %q", rows[0])
	}
	c2 := NewCanvas(2, 2)
	c2.SetDot(3, 7, 0) // bottom-right pixel → cell (1,1), dot bit 0x80
	rows2 := c2.Render()
	if !strings.Contains(rows2[1], "⢀") {
		t.Errorf("expected bottom-right dot ⢀ in row 1, got %q", rows2[1])
	}
}

func TestCanvasOutOfBoundsIgnored(t *testing.T) {
	c := NewCanvas(2, 2)
	c.SetDot(-1, 0, 0)
	c.SetDot(0, -5, 0)
	c.SetDot(100, 100, 0)
	for _, row := range c.Render() {
		if strings.TrimRight(row, " ") != "" && !strings.Contains(row, "\033") {
			t.Errorf("expected empty canvas, got %q", row)
		}
	}
}

func TestViewportRoundTrip(t *testing.T) {
	v := NewViewport(0, 0, 100, 50, 200, 100, 0)
	// world (0,0) should land bottom-left-ish, (100,50) top-right-ish
	x0, y0 := v.ToPixel(0, 0)
	x1, y1 := v.ToPixel(100, 50)
	if x1 <= x0 {
		t.Errorf("X should increase rightward: %d -> %d", x0, x1)
	}
	if y1 >= y0 {
		t.Errorf("Y should decrease upward (flipped): %d -> %d", y0, y1)
	}
}

func TestViewportUniformScale(t *testing.T) {
	// a 100x10 world in a square pixel area must use one scale for both axes
	v := NewViewport(0, 0, 100, 10, 100, 100, 0)
	x0, _ := v.ToPixel(0, 0)
	x1, _ := v.ToPixel(10, 0)
	_, y0 := v.ToPixel(0, 0)
	_, y1 := v.ToPixel(0, 10)
	dx := x1 - x0
	dy := y0 - y1
	if dx-dy > 1 || dy-dx > 1 {
		t.Errorf("10m should span equal pixels on both axes, got dx=%d dy=%d", dx, dy)
	}
}

func TestMapJSONRoundTrip(t *testing.T) {
	m := &MowerMap{
		FormatVersion: 1,
		Device:        "test",
		DownloadedAt:  time.Now().UTC(),
		Dock:          &DockPosition{X: 1.5, Y: -2.25, Toward: 900},
		Elements: []MapElement{
			{Hash: 42, Type: 0, TypeName: "area", Label: "Lawn",
				Points: []MapPoint{{X: 0.123, Y: 4.567}, {X: -8.9, Y: 10.11}}},
		},
	}
	path := filepath.Join(t.TempDir(), "m.json")
	if err := SaveMap(m, path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMap(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Device != "test" || len(got.Elements) != 1 || got.Dock == nil {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if got.Elements[0].Points[0].X != 0.123 {
		t.Errorf("precision lost: %v", got.Elements[0].Points[0])
	}
}

func TestMowerMapBounds(t *testing.T) {
	m := &MowerMap{
		Elements: []MapElement{
			{Points: []MapPoint{{X: -5, Y: 2}, {X: 10, Y: -3}}},
		},
		Dock: &DockPosition{X: 20, Y: 0},
	}
	minX, minY, maxX, maxY, ok := m.Bounds()
	if !ok || minX != -5 || maxX != 20 || minY != -3 || maxY != 2 {
		t.Errorf("bounds wrong: %v %v %v %v %v", minX, minY, maxX, maxY, ok)
	}
}

func TestOffScreenMarker(t *testing.T) {
	c := NewCanvas(40, 20)
	v := NewViewport(0, 0, 10, 10, c.PixelW(), c.PixelH(), 0)
	// on-screen point: no marker
	if DrawOffScreenMarker(c, v, 5, 5, colMower) {
		t.Error("point inside viewport should not be off-screen")
	}
	// far to the right and up
	if !DrawOffScreenMarker(c, v, 200, 5, colMower) {
		t.Error("point far right should be off-screen")
	}
	found := false
	for _, row := range c.Render() {
		if strings.Contains(row, "→") {
			found = true
		}
	}
	if !found {
		t.Error("expected a → marker pointing right toward off-screen mower")
	}
}

func TestDrawMapHidesLayer(t *testing.T) {
	m := &MowerMap{Elements: []MapElement{
		{Type: 0, Points: []MapPoint{{X: 0, Y: 0}, {X: 5, Y: 5}, {X: 5, Y: 0}}},
		{Type: 2, Points: []MapPoint{{X: 1, Y: 1}, {X: 4, Y: 1}}}, // path
	}}
	render := func(hidden map[int32]bool) string {
		c := NewCanvas(40, 20)
		v := NewViewport(0, 0, 5, 5, c.PixelW(), c.PixelH(), 0.05)
		DrawMap(c, v, m, hidden)
		return strings.Join(c.Render(), "")
	}
	full := render(nil)
	noPath := render(map[int32]bool{2: true})
	if full == noPath {
		t.Error("hiding the path layer should change the rendering")
	}
	// the gold path color must be absent when hidden, present when shown
	pathColor := fmt.Sprintf("\033[38;5;%dm", colPath)
	if !strings.Contains(full, pathColor) {
		t.Error("expected path color present when shown")
	}
	if strings.Contains(noPath, pathColor) {
		t.Error("expected no path color when path layer hidden")
	}
}

func TestRenderSnapshotDoesNotPanic(t *testing.T) {
	m := &MowerMap{Elements: []MapElement{{Type: 0, Points: []MapPoint{{X: 0, Y: 0}, {X: 5, Y: 5}, {X: 5, Y: 0}}}}}
	lines := renderMapSnapshot(m, 40, 20)
	if len(lines) != 20 {
		t.Errorf("expected 20 lines, got %d", len(lines))
	}
	// empty map should not panic either
	empty := &MowerMap{}
	if lines := renderMapSnapshot(empty, 40, 20); len(lines) == 0 {
		t.Error("expected placeholder output for empty map")
	}
}
