package main

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// trackDuration returns the best-effort length of an audio file in seconds.
// None of the tag formats we read carry duration, so this parses just enough
// of each container's header to compute it. Returns 0 if it can't be determined.
func trackDuration(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return flacDuration(f)
	case ".wav":
		return wavDuration(f)
	case ".aiff", ".aif":
		return aiffDuration(f)
	case ".m4a":
		return mp4Duration(f)
	case ".mp3":
		return mp3Duration(f)
	}
	return 0
}

func flacDuration(f *os.File) int {
	header := make([]byte, 4)
	if _, err := io.ReadFull(f, header); err != nil || string(header) != "fLaC" {
		return 0
	}
	for {
		blockHeader := make([]byte, 4)
		if _, err := io.ReadFull(f, blockHeader); err != nil {
			return 0
		}
		last := blockHeader[0]&0x80 != 0
		blockType := blockHeader[0] & 0x7f
		size := int(blockHeader[1])<<16 | int(blockHeader[2])<<8 | int(blockHeader[3])

		if blockType == 0 { // STREAMINFO
			info := make([]byte, size)
			if _, err := io.ReadFull(f, info); err != nil || len(info) < 18 {
				return 0
			}
			sampleRate := int(info[10])<<12 | int(info[11])<<4 | int(info[12])>>4
			totalSamples := int(info[13]&0x0f)<<32 | int(info[14])<<24 | int(info[15])<<16 | int(info[16])<<8 | int(info[17])
			if sampleRate == 0 {
				return 0
			}
			return totalSamples / sampleRate
		}

		if _, err := f.Seek(int64(size), io.SeekCurrent); err != nil {
			return 0
		}
		if last {
			return 0
		}
	}
}

func wavDuration(f *os.File) int {
	riff := make([]byte, 12)
	if _, err := io.ReadFull(f, riff); err != nil || string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return 0
	}
	var byteRate uint32
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(f, chunkHeader); err != nil {
			return 0
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])

		if chunkID == "fmt " {
			fmtChunk := make([]byte, chunkSize)
			if _, err := io.ReadFull(f, fmtChunk); err != nil || len(fmtChunk) < 16 {
				return 0
			}
			byteRate = binary.LittleEndian.Uint32(fmtChunk[8:12])
			continue
		}

		if chunkID == "data" {
			if byteRate == 0 {
				return 0
			}
			return int(chunkSize / byteRate)
		}

		if _, err := f.Seek(int64(chunkSize)+int64(chunkSize%2), io.SeekCurrent); err != nil {
			return 0
		}
	}
}

func aiffDuration(f *os.File) int {
	form := make([]byte, 12)
	if _, err := io.ReadFull(f, form); err != nil || string(form[0:4]) != "FORM" || string(form[8:12]) != "AIFF" {
		return 0
	}
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(f, chunkHeader); err != nil {
			return 0
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.BigEndian.Uint32(chunkHeader[4:8])

		if chunkID == "COMM" {
			comm := make([]byte, chunkSize)
			if _, err := io.ReadFull(f, comm); err != nil || len(comm) < 18 {
				return 0
			}
			numFrames := binary.BigEndian.Uint32(comm[2:6])
			sampleRate := int(parseIEEE80ExtendedSampleRate(comm[8:18]))
			if sampleRate == 0 {
				return 0
			}
			return int(numFrames) / sampleRate
		}

		if _, err := f.Seek(int64(chunkSize)+int64(chunkSize%2), io.SeekCurrent); err != nil {
			return 0
		}
	}
}

// parseIEEE80ExtendedSampleRate decodes the 80-bit IEEE extended float
// AIFF uses for sample rate, truncated to an int (good enough for typical rates).
func parseIEEE80ExtendedSampleRate(b []byte) uint64 {
	if len(b) < 10 {
		return 0
	}
	exponent := int(binary.BigEndian.Uint16(b[0:2]) & 0x7fff)
	mantissa := binary.BigEndian.Uint64(b[2:10])
	if exponent == 0 && mantissa == 0 {
		return 0
	}
	shift := exponent - 16383 - 63
	if shift >= 0 {
		return mantissa << uint(shift)
	}
	return mantissa >> uint(-shift)
}

// mp4Duration reads the moov/mvhd atom for overall duration/timescale.
func mp4Duration(f *os.File) int {
	stat, err := f.Stat()
	if err != nil {
		return 0
	}
	return findMP4Duration(f, 0, stat.Size())
}

func findMP4Duration(f *os.File, base, end int64) int {
	pos := base
	for pos < end {
		if _, err := f.Seek(pos, io.SeekStart); err != nil {
			return 0
		}
		header := make([]byte, 8)
		if _, err := io.ReadFull(f, header); err != nil {
			return 0
		}
		size := int64(binary.BigEndian.Uint32(header[0:4]))
		name := string(header[4:8])
		if size < 8 {
			return 0
		}

		if name == "moov" {
			if d := findMP4Duration(f, pos+8, pos+size); d > 0 {
				return d
			}
		}

		if name == "mvhd" {
			body := make([]byte, size-8)
			if _, err := io.ReadFull(f, body); err != nil || len(body) < 20 {
				return 0
			}
			version := body[0]
			if version == 1 {
				if len(body) < 28 {
					return 0
				}
				timescale := binary.BigEndian.Uint32(body[20:24])
				duration := binary.BigEndian.Uint64(body[24:32])
				if timescale == 0 {
					return 0
				}
				return int(duration / uint64(timescale))
			}
			timescale := binary.BigEndian.Uint32(body[12:16])
			duration := binary.BigEndian.Uint32(body[16:20])
			if timescale == 0 {
				return 0
			}
			return int(duration / timescale)
		}

		pos += size
	}
	return 0
}

// mp3Duration estimates length from the first frame's bitrate/samplerate and
// file size (uses the Xing/Info frame count when present for VBR accuracy).
func mp3Duration(f *os.File) int {
	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil || n < 4 {
		return 0
	}
	buf = buf[:n]

	offset := 0
	// Skip an ID3v2 header if present.
	if n >= 10 && string(buf[0:3]) == "ID3" {
		size := int(buf[6]&0x7f)<<21 | int(buf[7]&0x7f)<<14 | int(buf[8]&0x7f)<<7 | int(buf[9]&0x7f)
		offset = 10 + size
		if offset >= n {
			if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
				return 0
			}
			buf = make([]byte, 8192)
			n, err = f.Read(buf)
			if err != nil || n < 4 {
				return 0
			}
			buf = buf[:n]
			offset = 0
		}
	}

	for i := offset; i < len(buf)-4; i++ {
		if buf[i] != 0xff || buf[i+1]&0xe0 != 0xe0 {
			continue
		}
		bitrate, sampleRate, samplesPerFrame, ok := parseMP3FrameHeader(buf[i : i+4])
		if !ok {
			continue
		}

		stat, err := f.Stat()
		if err != nil || bitrate == 0 || sampleRate == 0 {
			return 0
		}

		// Check for a Xing/Info frame count, which gives an exact duration for VBR.
		if frames, ok := xingFrameCount(buf[i:]); ok && frames > 0 {
			return int(frames * samplesPerFrame / sampleRate)
		}

		return int(stat.Size()*8) / bitrate
	}
	return 0
}

func parseMP3FrameHeader(b []byte) (bitrate, sampleRate, samplesPerFrame int, ok bool) {
	if len(b) < 4 {
		return 0, 0, 0, false
	}
	versionBits := (b[1] >> 3) & 0x03
	layerBits := (b[1] >> 1) & 0x03
	bitrateIndex := (b[2] >> 4) & 0x0f
	sampleRateIndex := (b[2] >> 2) & 0x03
	if versionBits == 1 || layerBits == 0 || bitrateIndex == 0 || bitrateIndex == 0x0f || sampleRateIndex == 3 {
		return 0, 0, 0, false
	}

	mpeg1 := versionBits == 3
	var bitrates []int
	switch {
	case mpeg1 && layerBits == 3: // Layer I
		bitrates = []int{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448}
		samplesPerFrame = 384
	case mpeg1 && layerBits == 2: // Layer II
		bitrates = []int{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384}
		samplesPerFrame = 1152
	case mpeg1 && layerBits == 1: // Layer III
		bitrates = []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
		samplesPerFrame = 1152
	default: // MPEG2/2.5
		if layerBits == 3 {
			bitrates = []int{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256}
			samplesPerFrame = 384
		} else {
			bitrates = []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
			samplesPerFrame = 576 * 2
		}
	}

	sampleRates := map[byte][]int{
		3: {44100, 48000, 32000}, // MPEG1
		2: {22050, 24000, 16000}, // MPEG2
		0: {11025, 12000, 8000},  // MPEG2.5
	}
	rates, ok := sampleRates[versionBits]
	if !ok {
		return 0, 0, 0, false
	}
	return bitrates[bitrateIndex] * 1000, rates[sampleRateIndex], samplesPerFrame, true
}

func xingFrameCount(frame []byte) (int, bool) {
	for _, tag := range [][]byte{[]byte("Xing"), []byte("Info")} {
		idx := indexOf(frame, tag)
		if idx < 0 || idx+8 > len(frame) {
			continue
		}
		flags := binary.BigEndian.Uint32(frame[idx+4 : idx+8])
		if flags&0x01 == 0 || idx+12 > len(frame) {
			continue
		}
		return int(binary.BigEndian.Uint32(frame[idx+8 : idx+12])), true
	}
	return 0, false
}

func indexOf(haystack, needle []byte) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
