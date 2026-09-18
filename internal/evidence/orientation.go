package evidence

import (
	"encoding/binary"
	"image"
)

// jpegOrientation scans a JPEG's segment markers for an APP1 Exif block and
// returns the standard EXIF orientation value (1-8), or 1 (normal, no
// correction needed) if no Exif block, no orientation tag, or a malformed
// segment is found. Go's standard library deliberately has no EXIF support,
// so this reads only the one tag this package needs and nothing else — it
// is not a general EXIF parser, and does not try to be.
func jpegOrientation(data []byte) int {
	const normal = 1
	// A JPEG is a sequence of 0xFF-prefixed markers. SOI (Start Of Image) is
	// always first; everything until SOS (Start Of Scan, 0xDA) or the actual
	// image data is a segment we can walk past.
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return normal
	}
	pos := 2
	for pos+4 <= len(data) {
		if data[pos] != 0xFF {
			return normal // not a marker where we expected one; bail out safely
		}
		marker := data[pos+1]
		if marker == 0xD8 || marker == 0xD9 || (marker >= 0xD0 && marker <= 0xD7) {
			pos += 2 // markers with no length field
			continue
		}
		if marker == 0xDA {
			break // start of scan: image data follows, no more segments to check
		}
		if pos+4 > len(data) {
			return normal
		}
		segLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if segLen < 2 || pos+2+segLen > len(data) {
			return normal
		}
		if marker == 0xE1 { // APP1: where Exif lives
			seg := data[pos+4 : pos+2+segLen]
			if o, ok := parseExifOrientation(seg); ok {
				return o
			}
		}
		pos += 2 + segLen
	}
	return normal
}

// parseExifOrientation reads the orientation tag out of one APP1 payload
// that starts with the "Exif\0\0" header followed by a TIFF structure.
func parseExifOrientation(seg []byte) (int, bool) {
	if len(seg) < 8 || string(seg[0:4]) != "Exif" {
		return 0, false
	}
	tiff := seg[6:]
	if len(tiff) < 8 {
		return 0, false
	}
	var bo binary.ByteOrder
	switch string(tiff[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0, false
	}
	ifdOffset := bo.Uint32(tiff[4:8])
	if int(ifdOffset)+2 > len(tiff) {
		return 0, false
	}
	numEntries := bo.Uint16(tiff[ifdOffset : ifdOffset+2])
	const entrySize = 12
	base := int(ifdOffset) + 2
	for i := 0; i < int(numEntries); i++ {
		start := base + i*entrySize
		if start+entrySize > len(tiff) {
			break
		}
		tag := bo.Uint16(tiff[start : start+2])
		if tag == 0x0112 { // Orientation
			valType := bo.Uint16(tiff[start+2 : start+4])
			if valType != 3 { // SHORT
				return 0, false
			}
			v := bo.Uint16(tiff[start+8 : start+10])
			if v < 1 || v > 8 {
				return 0, false
			}
			return int(v), true
		}
	}
	return 0, false
}

// applyOrientation returns img rotated/flipped so its pixels match how the
// EXIF orientation tag says it should be displayed, so that discarding the
// tag afterward (as stripImageMetadata does) doesn't change what the image
// looks like. Orientation 1 (or an unrecognised value) is returned as-is.
//
// Values follow the EXIF standard: 2/4/5/7 mirror horizontally, 3/5/6/7/8
// rotate; see the switch below for the exact combinations.
func applyOrientation(img image.Image, orientation int) image.Image {
	switch orientation {
	case 2:
		return flipHorizontal(img)
	case 3:
		return rotate180(img)
	case 4:
		return flipVertical(img)
	case 5:
		return flipHorizontal(rotate90(img))
	case 6:
		return rotate90(img)
	case 7:
		return flipHorizontal(rotate270(img))
	case 8:
		return rotate270(img)
	default:
		return img
	}
}

func rotate90(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.Y-1-y, x, src.At(x, y))
		}
	}
	return dst
}

func rotate270(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(y, b.Max.X-1-x, src.At(x, y))
		}
	}
	return dst
}

func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.X-1-x, b.Max.Y-1-y, src.At(x, y))
		}
	}
	return dst
}

func flipHorizontal(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.X-1-x, y, src.At(x, y))
		}
	}
	return dst
}

func flipVertical(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x, b.Max.Y-1-y, src.At(x, y))
		}
	}
	return dst
}
