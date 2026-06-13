package cmd

import (
	"fmt"
	"math"
	"strings"
)

// Canvas is a braille-dot drawing surface. Each terminal cell holds a 2x4 dot
// matrix (U+2800 block), giving 8x the resolution of plain character plotting.
// Overlay runes (mower arrow, dock, labels) replace the braille in their cell.
type Canvas struct {
	W, H    int       // size in terminal cells
	dots    [][]uint8 // braille bit pattern per cell
	color   [][]int   // ANSI 256 color per cell (0 = default)
	overlay [][]rune  // non-braille glyphs drawn on top
	ovColor [][]int
}

// braille dot bit for sub-cell position (dx in 0..1, dy in 0..3)
var brailleBits = [4][2]uint8{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

func NewCanvas(w, h int) *Canvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	c := &Canvas{W: w, H: h}
	c.dots = make([][]uint8, h)
	c.color = make([][]int, h)
	c.overlay = make([][]rune, h)
	c.ovColor = make([][]int, h)
	for y := 0; y < h; y++ {
		c.dots[y] = make([]uint8, w)
		c.color[y] = make([]int, w)
		c.overlay[y] = make([]rune, w)
		c.ovColor[y] = make([]int, w)
	}
	return c
}

// PixelW/PixelH are the drawable resolution in braille dots.
func (c *Canvas) PixelW() int { return c.W * 2 }
func (c *Canvas) PixelH() int { return c.H * 4 }

// SetDot plots a single braille dot at pixel coordinates.
func (c *Canvas) SetDot(px, py int, col int) {
	if px < 0 || py < 0 || px >= c.PixelW() || py >= c.PixelH() {
		return
	}
	cx, cy := px/2, py/4
	c.dots[cy][cx] |= brailleBits[py%4][px%2]
	if col != 0 {
		c.color[cy][cx] = col
	}
}

// Line draws a braille line between two pixel coordinates (Bresenham).
func (c *Canvas) Line(x0, y0, x1, y1 int, col int) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	x, y := x0, y0
	for {
		c.SetDot(x, y, col)
		if x == x1 && y == y1 {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x += sx
		}
		if e2 < dx {
			err += dx
			y += sy
		}
	}
}

// SetOverlay places a glyph at cell coordinates, on top of any braille.
func (c *Canvas) SetOverlay(cx, cy int, r rune, col int) {
	if cx < 0 || cy < 0 || cx >= c.W || cy >= c.H {
		return
	}
	c.overlay[cy][cx] = r
	c.ovColor[cy][cx] = col
}

// OverlayString writes a horizontal string starting at cell coordinates.
func (c *Canvas) OverlayString(cx, cy int, s string, col int) {
	for i, r := range []rune(s) {
		c.SetOverlay(cx+i, cy, r, col)
	}
}

// Render returns one string per row with ANSI color escapes.
func (c *Canvas) Render() []string {
	rows := make([]string, c.H)
	var b strings.Builder
	for y := 0; y < c.H; y++ {
		b.Reset()
		cur := -1 // current color, -1 = unset
		for x := 0; x < c.W; x++ {
			var r rune
			var col int
			if c.overlay[y][x] != 0 {
				r = c.overlay[y][x]
				col = c.ovColor[y][x]
			} else if c.dots[y][x] != 0 {
				r = rune(0x2800 + int(c.dots[y][x]))
				col = c.color[y][x]
			} else {
				r = ' '
				col = 0
			}
			if col != cur {
				if col == 0 {
					b.WriteString("\033[0m")
				} else {
					fmt.Fprintf(&b, "\033[38;5;%dm", col)
				}
				cur = col
			}
			b.WriteRune(r)
		}
		if cur != 0 {
			b.WriteString("\033[0m")
		}
		rows[y] = b.String()
	}
	return rows
}

// Viewport maps world coordinates (meters) onto canvas pixels with a uniform
// scale (square meters stay square; braille dots are ~square at the usual 1:2
// terminal cell aspect).
type Viewport struct {
	MinX, MinY, MaxX, MaxY float64 // world bounds shown
	pxW, pxH               int
	scale                  float64 // pixels per meter
	offX, offY             float64
}

// NewViewport fits the world bounds into the pixel area, padded, centered.
func NewViewport(minX, minY, maxX, maxY float64, pxW, pxH int, padFrac float64) *Viewport {
	if maxX-minX < 1e-9 {
		minX -= 1
		maxX += 1
	}
	if maxY-minY < 1e-9 {
		minY -= 1
		maxY += 1
	}
	rx := maxX - minX
	ry := maxY - minY
	minX -= rx * padFrac
	maxX += rx * padFrac
	minY -= ry * padFrac
	maxY += ry * padFrac
	rx = maxX - minX
	ry = maxY - minY

	scale := math.Min(float64(pxW)/rx, float64(pxH)/ry)
	v := &Viewport{MinX: minX, MinY: minY, MaxX: maxX, MaxY: maxY, pxW: pxW, pxH: pxH, scale: scale}
	// center the content
	v.offX = (float64(pxW) - rx*scale) / 2
	v.offY = (float64(pxH) - ry*scale) / 2
	return v
}

// Zoom scales the viewport around its center. factor > 1 zooms in.
func (v *Viewport) Zoom(factor float64) {
	cx := (v.MinX + v.MaxX) / 2
	cy := (v.MinY + v.MaxY) / 2
	rx := (v.MaxX - v.MinX) / 2 / factor
	ry := (v.MaxY - v.MinY) / 2 / factor
	v.MinX, v.MaxX = cx-rx, cx+rx
	v.MinY, v.MaxY = cy-ry, cy+ry
	v.scale = math.Min(float64(v.pxW)/(v.MaxX-v.MinX), float64(v.pxH)/(v.MaxY-v.MinY))
	v.offX = (float64(v.pxW) - (v.MaxX-v.MinX)*v.scale) / 2
	v.offY = (float64(v.pxH) - (v.MaxY-v.MinY)*v.scale) / 2
}

// Pan shifts the viewport by a fraction of its size.
func (v *Viewport) Pan(fracX, fracY float64) {
	dx := (v.MaxX - v.MinX) * fracX
	dy := (v.MaxY - v.MinY) * fracY
	v.MinX += dx
	v.MaxX += dx
	v.MinY += dy
	v.MaxY += dy
}

// MetersPerPixel reports the current resolution.
func (v *Viewport) MetersPerPixel() float64 {
	if v.scale == 0 {
		return 0
	}
	return 1 / v.scale
}

// ToPixel converts world meters to pixel coordinates (Y axis flipped so north
// is up).
func (v *Viewport) ToPixel(x, y float64) (int, int) {
	px := (x-v.MinX)*v.scale + v.offX
	py := (y-v.MinY)*v.scale + v.offY
	return int(math.Round(px)), v.pxH - 1 - int(math.Round(py))
}

// Colors (ANSI 256)
const (
	colArea     = 40  // green — mowing area boundary
	colObstacle = 196 // red — obstacle
	colPath     = 178 // gold — channel/path
	colTrailOld = 24  // dim blue — older trail
	colTrailNew = 39  // bright blue — recent trail
	colMower    = 231 // white
	colDock     = 201 // magenta
	colLabel    = 250 // grey
	colPlanned  = 51  // cyan — planned coverage (zigzag) path
)

// elementColor maps a map element type to its render color.
func elementColor(t int32) int {
	switch t {
	case 0:
		return colArea
	case 1:
		return colObstacle
	case 2:
		return colPath
	default:
		return colLabel
	}
}

// elementClosed reports whether the element's polyline should be closed.
func elementClosed(t int32) bool {
	return t == 0 || t == 1 // areas and obstacles are polygons
}

// DrawMap renders the elements of a MowerMap onto the canvas through the
// viewport. Element types present in hidden are skipped (nil = draw all).
func DrawMap(c *Canvas, v *Viewport, m *MowerMap, hidden map[int32]bool) {
	for _, el := range m.Elements {
		if hidden[el.Type] {
			continue
		}
		col := elementColor(el.Type)
		n := len(el.Points)
		if n == 0 {
			continue
		}
		if n == 1 {
			px, py := v.ToPixel(el.Points[0].X, el.Points[0].Y)
			c.SetDot(px, py, col)
			continue
		}
		last := n - 1
		for i := 0; i < last; i++ {
			x0, y0 := v.ToPixel(el.Points[i].X, el.Points[i].Y)
			x1, y1 := v.ToPixel(el.Points[i+1].X, el.Points[i+1].Y)
			c.Line(x0, y0, x1, y1, col)
		}
		if elementClosed(el.Type) {
			x0, y0 := v.ToPixel(el.Points[last].X, el.Points[last].Y)
			x1, y1 := v.ToPixel(el.Points[0].X, el.Points[0].Y)
			c.Line(x0, y0, x1, y1, col)
		}
		if el.Label != "" {
			// label at polygon centroid
			var sx, sy float64
			for _, p := range el.Points {
				sx += p.X
				sy += p.Y
			}
			px, py := v.ToPixel(sx/float64(n), sy/float64(n))
			c.OverlayString(px/2, py/4, el.Label, colLabel)
		}
	}
	dock, _ := m.DockEstimate()
	px, py := v.ToPixel(dock.X, dock.Y)
	c.SetOverlay(px/2, py/4, '⌂', colDock)
}

// DrawTrail renders the mower's path history; the most recent segment is
// brighter.
func DrawTrail(c *Canvas, v *Viewport, trail []MapPoint) {
	n := len(trail)
	for i := 1; i < n; i++ {
		col := colTrailOld
		if i > n-20 {
			col = colTrailNew
		}
		x0, y0 := v.ToPixel(trail[i-1].X, trail[i-1].Y)
		x1, y1 := v.ToPixel(trail[i].X, trail[i].Y)
		c.Line(x0, y0, x1, y1, col)
	}
}

// DrawPolyline renders an open polyline (e.g. the planned coverage path).
func DrawPolyline(c *Canvas, v *Viewport, pts []MapPoint, col int) {
	for i := 1; i < len(pts); i++ {
		x0, y0 := v.ToPixel(pts[i-1].X, pts[i-1].Y)
		x1, y1 := v.ToPixel(pts[i].X, pts[i].Y)
		c.Line(x0, y0, x1, y1, col)
	}
}

// DrawMower places the mower arrow at world position with compass heading
// (0° = north, clockwise).
func DrawMower(c *Canvas, v *Viewport, x, y, headingDeg float64) {
	px, py := v.ToPixel(x, y)
	c.SetOverlay(px/2, py/4, headingArrow(headingDeg), colMower)
}

// DrawOffScreenMarker draws an arrow clamped to the canvas border pointing
// toward a world position that falls outside the viewport, so a mower that has
// driven (or mis-aligned) off the visible map is never silently lost.
// Returns true if the target was off-screen.
func DrawOffScreenMarker(c *Canvas, v *Viewport, x, y float64, col int) bool {
	px, py := v.ToPixel(x, y)
	cx, cy := px/2, py/4
	if cx >= 0 && cx < c.W && cy >= 0 && cy < c.H {
		return false
	}
	// Clamp to border and choose an arrow pointing outward.
	clampedX, clampedY := cx, cy
	if clampedX < 0 {
		clampedX = 0
	} else if clampedX >= c.W {
		clampedX = c.W - 1
	}
	if clampedY < 0 {
		clampedY = 0
	} else if clampedY >= c.H {
		clampedY = c.H - 1
	}
	dx := cx - clampedX
	dy := cy - clampedY
	adx, ady := dx, dy
	if adx < 0 {
		adx = -adx
	}
	if ady < 0 {
		ady = -ady
	}
	var arrow rune
	switch {
	case dy < 0 && ady >= adx:
		arrow = '↑'
	case dy > 0 && ady >= adx:
		arrow = '↓'
	case dx < 0:
		arrow = '←'
	default:
		arrow = '→'
	}
	c.SetOverlay(clampedX, clampedY, arrow, col)
	return true
}

func headingArrow(heading float64) rune {
	heading = math.Mod(heading, 360)
	if heading < 0 {
		heading += 360
	}
	arrows := []rune{'↑', '↗', '→', '↘', '↓', '↙', '←', '↖'}
	idx := int(math.Round(heading/45)) % 8
	return arrows[idx]
}
