package attachments

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var ErrWebP = errors.New("invalid WebP container structure")

type WebP struct {
	Width, Height, Frames int
	Durations             []int
	Animated              bool
}
type webpChunk struct {
	tag  string
	data []byte
}

func chunks(data []byte) ([]webpChunk, error) {
	var out []webpChunk
	for len(data) > 0 {
		if len(data) < 8 {
			return nil, ErrWebP
		}
		n := uint64(binary.LittleEndian.Uint32(data[4:8]))
		size := uint64(8) + n + (n & 1)
		if size > uint64(len(data)) {
			return nil, ErrWebP
		}
		if n&1 == 1 && data[8+n] != 0 {
			return nil, ErrWebP
		}
		out = append(out, webpChunk{tag: string(data[:4]), data: data[8 : 8+n]})
		data = data[size:]
	}
	return out, nil
}
func uint24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }
func webpImage(rows []webpChunk) (int, int, error) {
	images := 0
	width, height := 0, 0
	alpha := false
	for _, c := range rows {
		switch c.tag {
		case "ALPH":
			if alpha || images != 0 || len(c.data) == 0 {
				return 0, 0, ErrWebP
			}
			alpha = true
		case "VP8 ":
			if images != 0 || len(c.data) < 10 || c.data[0]&1 != 0 || !bytes.Equal(c.data[3:6], []byte{0x9d, 0x01, 0x2a}) {
				return 0, 0, ErrWebP
			}
			width = int(binary.LittleEndian.Uint16(c.data[6:8]) & 0x3fff)
			height = int(binary.LittleEndian.Uint16(c.data[8:10]) & 0x3fff)
			images++
		case "VP8L":
			if images != 0 || alpha || len(c.data) < 5 || c.data[0] != 0x2f {
				return 0, 0, ErrWebP
			}
			bits := binary.LittleEndian.Uint32(c.data[1:5])
			if bits>>29 != 0 {
				return 0, 0, ErrWebP
			}
			width = int(bits&0x3fff) + 1
			height = int(bits>>14&0x3fff) + 1
			images++
		default:
			return 0, 0, ErrWebP
		}
	}
	if images != 1 || width <= 0 || height <= 0 {
		return 0, 0, ErrWebP
	}
	return width, height, nil
}

// InspectWebP validates RIFF lengths, chunk ownership/order, canvas/frame
// bounds and timing. Full pixel decoding is an additional codec validation.
func InspectWebP(data []byte) (WebP, error) {
	var info WebP
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return info, ErrWebP
	}
	rows, err := chunks(data[12:])
	if err != nil || len(rows) == 0 {
		return info, ErrWebP
	}
	extended := rows[0].tag == "VP8X"
	flags := byte(0)
	start := 0
	if extended {
		if len(rows[0].data) != 10 {
			return info, ErrWebP
		}
		v := rows[0].data
		if v[0]&0xc1 != 0 || v[1] != 0 || v[2] != 0 || v[3] != 0 {
			return info, ErrWebP
		}
		flags = v[0]
		info.Width, info.Height = uint24(v[4:7])+1, uint24(v[7:10])+1
		info.Animated = flags&2 != 0
		start = 1
	}
	seen := map[string]bool{}
	var image []webpChunk
	animationHeader := false
	for _, c := range rows[start:] {
		switch c.tag {
		case "VP8X":
			return info, ErrWebP
		case "ICCP", "EXIF", "XMP ":
			if !extended || seen[c.tag] || (c.tag == "ICCP" && (len(image) > 0 || info.Frames > 0)) {
				return info, ErrWebP
			}
			seen[c.tag] = true
		case "ANIM":
			if !extended || !info.Animated || animationHeader || info.Frames > 0 || len(c.data) != 6 {
				return info, ErrWebP
			}
			animationHeader = true
		case "ANMF":
			if !animationHeader || !info.Animated || len(c.data) < 16 || c.data[15]&0xfc != 0 {
				return info, ErrWebP
			}
			v := c.data
			x, y := uint24(v[:3])*2, uint24(v[3:6])*2
			w, h := uint24(v[6:9])+1, uint24(v[9:12])+1
			duration := uint24(v[12:15])
			if x+w > info.Width || y+h > info.Height || duration <= 0 {
				return info, ErrWebP
			}
			sub, err := chunks(v[16:])
			if err != nil {
				return info, err
			}
			actualW, actualH, err := webpImage(sub)
			if err != nil || actualW != w || actualH != h {
				return info, ErrWebP
			}
			info.Frames++
			info.Durations = append(info.Durations, duration)
		case "VP8 ", "VP8L", "ALPH":
			if info.Animated || (c.tag == "ALPH" && !extended) {
				return info, ErrWebP
			}
			image = append(image, c)
		default: // RIFF permits application chunks; their complete sizes were checked.
		}
	}
	if info.Animated {
		if info.Frames == 0 || len(image) > 0 {
			return info, ErrWebP
		}
	} else {
		width, height, err := webpImage(image)
		if err != nil {
			return info, err
		}
		if extended && (info.Width != width || info.Height != height) {
			return info, ErrWebP
		}
		info.Width, info.Height, info.Frames = width, height, 1
	}
	for _, metadata := range []struct {
		tag  string
		flag byte
	}{{"ICCP", 0x20}, {"EXIF", 8}, {"XMP ", 4}} {
		if (flags&metadata.flag != 0) != seen[metadata.tag] {
			return info, ErrWebP
		}
	}
	if !extended && (animationHeader || info.Animated) {
		return info, ErrWebP
	}
	return info, nil
}
