package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/ThalesMMS/dicom-go/object"
	"github.com/ThalesMMS/dicom-go/pixeldata"
	"github.com/ThalesMMS/dicom-go/pixeldata/display"
	"github.com/ThalesMMS/dicom-go/pixeldata/jpeg"
	"github.com/ThalesMMS/dicom-go/pixeldata/jpeglossless"
	"github.com/ThalesMMS/dicom-go/pixeldata/rle"
)

var ErrPreviewUnsupported = errors.New("DICOM preview unsupported")

func (s *Session) PreviewInstancePNG(ctx context.Context, sopInstanceUID string, size string) ([]byte, error) {
	instance, err := s.catalog.InstanceBySOPInstanceUID(ctx, sopInstanceUID)
	if err != nil {
		return nil, err
	}
	size = normalizePreviewSize(size)
	cachePath := filepath.Join(s.archiveDir, "preview-cache", instance.SHA256+"-"+size+".png")
	if data, err := os.ReadFile(cachePath); err == nil {
		return data, nil
	}
	data, err := renderPreviewPNG(instance.StoredPath, previewMaxDimension(size))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
		_ = os.WriteFile(cachePath, data, 0o644)
	}
	return data, nil
}

func normalizePreviewSize(size string) string {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "large":
		return "large"
	default:
		return "thumb"
	}
}

func previewMaxDimension(size string) int {
	if size == "large" {
		return 1024
	}
	return 256
}

func renderPreviewPNG(path string, maxDimension int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	dicomFile, err := object.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPreviewUnsupported, err)
	}
	img, err := renderPreviewImage(dicomFile)
	if err != nil {
		return nil, err
	}
	img = resizeNearest(img, maxDimension)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func renderPreviewImage(file *object.File) (image.Image, error) {
	if file == nil || file.Dataset == nil {
		return nil, ErrPreviewUnsupported
	}
	pixel, err := pixeldata.Extract(file.Dataset)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPreviewUnsupported, err)
	}
	metadata, err := pixeldata.ExtractMetadata(file.Dataset)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPreviewUnsupported, err)
	}
	registry, err := previewPixelRegistry()
	if err != nil {
		return nil, err
	}
	frames, err := registry.DecodeFrames(file.TransferSyntax.UID, pixel, file.Dataset)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPreviewUnsupported, err)
	}
	if len(frames.Data) == 0 {
		return nil, ErrPreviewUnsupported
	}
	format := display.PixelFormat{
		BitsAllocated: int(metadata.BitsAllocated),
		BitsStored:    int(metadata.BitsStored),
		HighBit:       int(metadata.HighBit),
		Signed:        metadata.PixelRepresentation != 0,
		ByteOrder:     file.TransferSyntax.ByteOrder,
	}
	if metadata.SamplesPerPixel == 1 {
		modality, _ := display.ModalityLUTFromObject(file.Dataset)
		voi, _ := display.VOIFromObject(file.Dataset)
		img, err := display.RenderGray(display.Frame{
			Rows:     int(metadata.Rows),
			Columns:  int(metadata.Columns),
			Pixels:   frames.Data[0],
			Format:   format,
			Modality: modality,
			VOI:      voi,
		})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPreviewUnsupported, err)
		}
		return img, nil
	}
	colorMetadata := display.ColorMetadataFromObject(file.Dataset)
	palette, err := display.PaletteFromObject(file.Dataset)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPreviewUnsupported, err)
	}
	img, err := display.RenderColor(display.ColorFrame{
		Rows:                int(metadata.Rows),
		Columns:             int(metadata.Columns),
		Pixels:              frames.Data[0],
		Photometric:         colorMetadata.Photometric,
		SamplesPerPixel:     int(metadata.SamplesPerPixel),
		PlanarConfiguration: colorMetadata.PlanarConfiguration,
		Format:              format,
		Palette:             palette,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPreviewUnsupported, err)
	}
	return img, nil
}

func previewPixelRegistry() (pixeldata.Registry, error) {
	registry := pixeldata.NewMemoryRegistry()
	for _, register := range []func(pixeldata.Registry) error{
		jpeg.Register,
		jpeglossless.Register,
		rle.Register,
	} {
		if err := register(registry); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func resizeNearest(src image.Image, maxDimension int) image.Image {
	if src == nil || maxDimension <= 0 {
		return src
	}
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 || (width <= maxDimension && height <= maxDimension) {
		return src
	}
	dstW, dstH := width, height
	if width >= height {
		dstW = maxDimension
		dstH = max(1, height*maxDimension/width)
	} else {
		dstH = maxDimension
		dstW = max(1, width*maxDimension/height)
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		srcY := bounds.Min.Y + y*height/dstH
		for x := 0; x < dstW; x++ {
			srcX := bounds.Min.X + x*width/dstW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}
