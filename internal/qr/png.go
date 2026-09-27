package qr

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
)

// borderModules is the quiet zone (margin) around the QR code, in modules.
const borderModules = 4

// PNG writes the QR code to path as a PNG image. Each module is rendered as a
// scale×scale pixel square (scale of 1 produces a tiny image; 6 is readable).
// A 4-module quiet zone surrounds the code.
func (m *Matrix) PNG(path string, scale int) error {
	if scale < 1 {
		scale = 6
	}
	size := m.Size + 2*borderModules
	px := size * scale
	img := image.NewRGBA(image.Rect(0, 0, px, px))

	// white background
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	// modules
	for y := 0; y < m.Size; y++ {
		for x := 0; x < m.Size; x++ {
			if !m.Dark[y][x] {
				continue
			}
			x0 := (x + borderModules) * scale
			y0 := (y + borderModules) * scale
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.Set(x0+dx, y0+dy, color.RGBA{0, 0, 0, 255})
				}
			}
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	return f.Close()
}

// PNGTo writes the QR code as a PNG to the given writer (scale defaults to 6).
func (m *Matrix) PNGTo(w io.Writer, scale int) error {
	if scale < 1 {
		scale = 1
	}
	size := m.Size + 2*borderModules
	px := size * scale
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			img.Set(x, y, white)
		}
	}
	for y := 0; y < m.Size; y++ {
		for x := 0; x < m.Size; x++ {
			if !m.Dark[y][x] {
				continue
			}
			x0 := (x + borderModules) * scale
			y0 := (y + borderModules) * scale
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.Set(x0+dx, y0+dy, black)
				}
			}
		}
	}
	return png.Encode(w, img)
}
