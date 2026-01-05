package utils

// ConvertBase64ToImage converts a base64 string to an image and saves it to the specified path.
// It returns the path where the image is saved or an error if the conversion fails.
import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"
)

func ConvertBase64ToImageSave(base64Str, path string) (string, error) {
	outputPath := "storage/public/" + path + ".png"
	// Split the base64 string to get the actual data
	parts := strings.SplitN(base64Str, ",", 2)
	if len(parts) != 2 {
		return "", errors.New("invalid base64 string")
	}
	data := parts[1]

	// Decode the base64 string
	decodedData, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}

	// Decode the image
	img, format, err := image.Decode(strings.NewReader(string(decodedData)))

	if err != nil {
		return "", err
	}

	// Create the output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return "", err
	}
	defer outFile.Close()

	// Ensure is png
	if format != "png" {
		// Convert to png
		bounds := img.Bounds()
		rgba := image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
		img = rgba
		// format = "png"
	}
	// Encode and save the image in the appropriate format
	// switch format {
	// case "png":
	err = png.Encode(outFile, img)
	// case "jpeg":
	// err = jpeg.Encode(outFile, img, nil)
	// case "gif":
	// err = errors.New("GIF format not supported for saving")
	// default:
	// err = errors.New("unsupported image format")
	// }
	if err != nil {
		return "", err
	}

	return outputPath, nil
}

func ComposeMockup(screenshot []byte) ([]byte, error) {
	mockupFile, err := os.Open("storage/mockup.png")
	if err != nil {
		return nil, err
	}
	defer mockupFile.Close()

	mockupImg, err := png.Decode(mockupFile)
	if err != nil {
		fmt.Printf("Error decoding mockup image: %v\n", err)
		return nil, err
	}

	// Decode screenshot
	screenshotImg, err := png.Decode(bytes.NewReader(screenshot))
	if err != nil {
		fmt.Printf("Error decoding screenshot image: %v\n", err)
		return nil, err
	}

	rounded := RoundCornersRGBA(screenshotImg, 62)

	// Final canvas (RGBA)
	final := image.NewRGBA(mockupImg.Bounds())

	// 1) Put screenshot FIRST (underlay)
	screenRect := image.Rect(68, 58, 68+1290, 58+2800)
	draw.Draw(final, screenRect, rounded, image.Point{0, 0}, draw.Src)

	// 2) Put mockup OVERLAY (transparent areas allow screenshot to show)
	draw.Draw(final, final.Bounds(), mockupImg, image.Point{0, 0}, draw.Over)

	// Encode output as PNG bytes
	var buf bytes.Buffer
	if err := png.Encode(&buf, final); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func RoundCornersRGBA(img image.Image, radius int) *image.RGBA {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	dst := image.NewRGBA(bounds)

	// radius için maske alanı
	r := float64(radius)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {

			// Noktanın hangi köşeye denk geldiğini bul
			var dx, dy float64

			switch {
			case x < radius && y < radius: // top-left
				dx = float64(radius - x)
				dy = float64(radius - y)

			case x > w-radius && y < radius: // top-right
				dx = float64(x - (w - radius))
				dy = float64(radius - y)

			case x < radius && y > h-radius: // bottom-left
				dx = float64(radius - x)
				dy = float64(y - (h - radius))

			case x > w-radius && y > h-radius: // bottom-right
				dx = float64(x - (w - radius))
				dy = float64(y - (h - radius))

			default:
				// köşe değil, direkt al
				dst.Set(x, y, img.At(x, y))
				continue
			}

			// Çember dışındaysa α=0 → pixel transparent
			if dx*dx+dy*dy > r*r {
				dst.Set(x, y, color.RGBA{0, 0, 0, 0})
			} else {
				dst.Set(x, y, img.At(x, y))
			}
		}
	}

	return dst
}
