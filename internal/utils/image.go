package utils

// ConvertBase64ToImage converts a base64 string to an image and saves it to the specified path.
// It returns the path where the image is saved or an error if the conversion fails.
import (
	"encoding/base64"
	"errors"
	"image"
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
