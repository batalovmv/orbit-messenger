// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"

	_ "image/gif"
	_ "image/png"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

// ImageResult holds processed image variants.
type ImageResult struct {
	Original  []byte // re-encoded JPEG (stripped EXIF)
	Thumb320  []byte // 320px thumbnail
	Medium800 []byte // 800px medium
	Width     int
	Height    int
}

// VideoResult holds extracted video metadata and thumbnail.
type VideoResult struct {
	Thumbnail []byte
	Duration  float64
	Width     int
	Height    int
}

// AudioResult holds voice processing results.
type AudioResult struct {
	WaveformPeaks []byte  // 100 values 0-31
	Duration      float64 // seconds
}

// ProcessImage resizes a photo: original (EXIF-stripped) + thumb_320 + medium_800.
// Input: raw file bytes. Output: JPEG variants.
func ProcessImage(data []byte) (*ImageResult, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Thumbnail 320px (fit, not crop)
	thumb := imaging.Fit(img, 320, 320, imaging.Lanczos)
	var thumbBuf bytes.Buffer
	if err := jpeg.Encode(&thumbBuf, thumb, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("encode thumb: %w", err)
	}

	// Medium 800px
	medium := imaging.Fit(img, 800, 800, imaging.Lanczos)
	var medBuf bytes.Buffer
	if err := jpeg.Encode(&medBuf, medium, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode medium: %w", err)
	}

	// Original re-encoded as JPEG (strips EXIF)
	var origBuf bytes.Buffer
	if err := jpeg.Encode(&origBuf, img, &jpeg.Options{Quality: 92}); err != nil {
		return nil, fmt.Errorf("encode original: %w", err)
	}

	return &ImageResult{
		Original:  origBuf.Bytes(),
		Thumb320:  thumbBuf.Bytes(),
		Medium800: medBuf.Bytes(),
		Width:     w,
		Height:    h,
	}, nil
}

// ExtractVideoThumbnail extracts the first frame of a video as JPEG.
func ExtractVideoThumbnail(inputPath string) ([]byte, error) {
	if !ffmpegAvailable() {
		return nil, fmt.Errorf("ffmpeg not available")
	}

	cmd := exec.Command("ffmpeg",
		"-i", inputPath,
		"-vframes", "1",
		"-ss", "00:00:01",
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"-q:v", "5",
		"pipe:1",
	)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Try without -ss (very short videos)
		cmd2 := exec.Command("ffmpeg",
			"-i", inputPath,
			"-vframes", "1",
			"-f", "image2pipe",
			"-vcodec", "mjpeg",
			"-q:v", "5",
			"pipe:1",
		)
		var out2 bytes.Buffer
		cmd2.Stdout = &out2
		cmd2.Stderr = io.Discard
		if err2 := cmd2.Run(); err2 != nil {
			return nil, fmt.Errorf("ffmpeg thumbnail: %w (stderr: %s)", err, stderr.String())
		}
		return out2.Bytes(), nil
	}
	return out.Bytes(), nil
}

// GetVideoMetadata extracts duration, width, height using ffprobe.
func GetVideoMetadata(inputPath string) (duration float64, width, height int, err error) {
	if !ffprobeAvailable() {
		return 0, 0, 0, fmt.Errorf("ffprobe not available")
	}

	// Duration
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("ffprobe duration: %w", err)
	}
	duration, _ = strconv.ParseFloat(strings.TrimSpace(string(out)), 64)

	// Resolution
	cmd2 := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		inputPath,
	)
	out2, err := cmd2.Output()
	if err != nil {
		return duration, 0, 0, nil // duration ok, resolution unknown
	}
	parts := strings.Split(strings.TrimSpace(string(out2)), "x")
	if len(parts) == 2 {
		width, _ = strconv.Atoi(parts[0])
		height, _ = strconv.Atoi(parts[1])
	}
	return duration, width, height, nil
}

// ExtractWaveform generates waveform peak values from an audio file.
// Returns ~100 peak values (0-31) and duration.
func ExtractWaveform(inputPath string) (*AudioResult, error) {
	// Get duration first
	duration := 0.0
	if ffprobeAvailable() {
		cmd := exec.Command("ffprobe",
			"-v", "error",
			"-show_entries", "format=duration",
			"-of", "default=noprint_wrappers=1:nokey=1",
			inputPath,
		)
		out, err := cmd.Output()
		if err == nil {
			duration, _ = strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
		}
	}

	if !ffmpegAvailable() {
		// Return flat waveform if ffmpeg not available
		peaks := make([]byte, 100)
		for i := range peaks {
			peaks[i] = 15
		}
		return &AudioResult{WaveformPeaks: peaks, Duration: duration}, nil
	}

	// Convert to raw PCM s16le mono 8kHz — write to temp file to avoid OOM
	pcmFile, err := os.CreateTemp("", "orbit-pcm-*.raw")
	if err != nil {
		peaks := make([]byte, 100)
		for i := range peaks {
			peaks[i] = 15
		}
		return &AudioResult{WaveformPeaks: peaks, Duration: duration}, nil
	}
	pcmPath := pcmFile.Name()
	pcmFile.Close()
	defer os.Remove(pcmPath)

	cmd := exec.Command("ffmpeg",
		"-i", inputPath,
		"-ac", "1",
		"-ar", "8000",
		"-f", "s16le",
		pcmPath,
		"-y",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		peaks := make([]byte, 100)
		for i := range peaks {
			peaks[i] = 15
		}
		return &AudioResult{WaveformPeaks: peaks, Duration: duration}, nil
	}

	fi, err := os.Stat(pcmPath)
	if err != nil || fi.Size() == 0 {
		return &AudioResult{WaveformPeaks: make([]byte, 100), Duration: duration}, nil
	}

	numSamples := int(fi.Size()) / 2 // 16-bit = 2 bytes
	if numSamples == 0 {
		return &AudioResult{WaveformPeaks: make([]byte, 100), Duration: duration}, nil
	}

	const numPeaks = 100
	samplesPerPeak := numSamples / numPeaks
	if samplesPerPeak < 1 {
		samplesPerPeak = 1
	}

	peaks := make([]byte, numPeaks)
	maxPeak := 0.0
	rawPeaks := make([]float64, numPeaks)

	pcmReader, err := os.Open(pcmPath)
	if err != nil {
		return &AudioResult{WaveformPeaks: peaks, Duration: duration}, nil
	}
	defer pcmReader.Close()

	const chunkBytes = 64 * 1024 // 64 KB read buffer
	buf := make([]byte, chunkBytes)
	byteOffset := 0 // absolute byte position in PCM stream

	for {
		n, readErr := pcmReader.Read(buf)
		if n == 0 {
			break
		}
		chunk := buf[:n]
		// Process each 2-byte sample in this chunk
		for k := 0; k+1 < len(chunk); k += 2 {
			sampleIndex := (byteOffset + k) / 2
			peakIndex := sampleIndex / samplesPerPeak
			if peakIndex >= numPeaks {
				break
			}
			sample := int16(binary.LittleEndian.Uint16(chunk[k : k+2]))
			abs := math.Abs(float64(sample))
			if abs > rawPeaks[peakIndex] {
				rawPeaks[peakIndex] = abs
				if abs > maxPeak {
					maxPeak = abs
				}
			}
		}
		byteOffset += n
		if readErr != nil {
			break
		}
	}

	// Normalize to 0-31
	for i, p := range rawPeaks {
		if maxPeak > 0 {
			peaks[i] = byte(p / maxPeak * 31)
		}
	}

	return &AudioResult{WaveformPeaks: peaks, Duration: duration}, nil
}

// ConvertGIFToMP4 converts a GIF to MP4 using ffmpeg.
func ConvertGIFToMP4(inputPath, outputPath string) error {
	if !ffmpegAvailable() {
		return fmt.Errorf("ffmpeg not available")
	}
	cmd := exec.Command("ffmpeg",
		"-i", inputPath,
		"-movflags", "+faststart",
		"-pix_fmt", "yuv420p",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-y",
		outputPath,
	)
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func ffmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		slog.Warn("ffmpeg not found in PATH")
	}
	return err == nil
}

func ffprobeAvailable() bool {
	_, err := exec.LookPath("ffprobe")
	return err == nil
}

// SaveToTemp writes data to a temporary file and returns the path.
func SaveToTemp(data []byte, prefix string) (string, error) {
	f, err := os.CreateTemp("", prefix+"-*")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}
