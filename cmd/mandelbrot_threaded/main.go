// Command mandelbrot_threaded generates Mandelbrot set visualizations in
// ASCII or for gnuplot, splitting row computation across a fixed pool of
// goroutines. This is a direct port of the C reference's
// mandelbrot_pthread.c, self-contained like its sibling C files (no
// shared package between variants, matching the C repo's own layout).
//
// Build:
//
//	go build -o mandelbrot_threaded ./cmd/mandelbrot_threaded
//
// Usage:
//
//	./mandelbrot_threaded
//	./mandelbrot_threaded width=120 ll_x=-0.75 ll_y=0.1 ur_x=-0.74 ur_y=0.11
//	./mandelbrot_threaded png=1 width=800 height=600 > mandelbrot.dat
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// NumThreads matches the C reference's fixed worker-pool size
// (mandelbrot_pthread.c's NUM_THREADS).
const NumThreads = 10

// ChunkSize is the number of rows claimed per atomic fetch-add - matches
// the C reference's CHUNK_SIZE (1 row per claim).
const ChunkSize = 1

// Config holds the render parameters, mirroring the C Config struct.
type Config struct {
	Width   int
	Height  int
	PNG     bool
	LLx     float64
	LLy     float64
	URx     float64
	URy     float64
	MaxIter int
}

// symbols maps iteration counts to ASCII characters, darkest (in the set)
// to lightest (escapes immediately).
const symbols = "MW2a_. "

// cnt2char maps an iteration value (0 to maxIter) to an ASCII character.
func cnt2char(value, maxIter int) byte {
	ns := len(symbols)
	idx := int(float64(value) / float64(maxIter) * float64(ns-1))
	return symbols[idx]
}

// escapeTime calculates the escape time for a point c = cr + ci*i in the
// complex plane, returning maxIter minus the number of iterations taken
// to escape.
func escapeTime(cr, ci float64, maxIter int) int {
	zr, zi := 0.0, 0.0
	iter := 0
	for ; iter < maxIter; iter++ {
		zr2 := zr * zr
		zi2 := zi * zi
		if zr2+zi2 > 4.0 {
			break
		}
		tmp := zr2 - zi2 + cr
		zi = 2.0*zr*zi + ci
		zr = tmp
	}
	return maxIter - iter
}

// parseArg parses a single "key=value" command-line argument, mirroring
// the C reference's parse_arg.
func parseArg(arg string, cfg *Config) {
	key, value, ok := strings.Cut(arg, "=")
	if !ok {
		fmt.Fprintf(os.Stderr, "Warning: Ignoring invalid argument '%s'\n", arg)
		return
	}
	switch key {
	case "width":
		cfg.Width = atoiC(value)
	case "height":
		cfg.Height = atoiC(value)
	case "png":
		cfg.PNG = atoiC(value) != 0
	case "ll_x":
		cfg.LLx = atofC(value)
	case "ll_y":
		cfg.LLy = atofC(value)
	case "ur_x":
		cfg.URx = atofC(value)
	case "ur_y":
		cfg.URy = atofC(value)
	case "max_iter":
		cfg.MaxIter = atoiC(value)
	default:
		fmt.Fprintf(os.Stderr, "Warning: Unknown parameter '%s'\n", key)
	}
}

// atoiC behaves like C's atoi: parse a leading optional-sign integer
// prefix, ignore trailing garbage, and return 0 if nothing parses.
func atoiC(s string) int {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digitsStart := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == digitsStart {
		return 0
	}
	n, _ := strconv.Atoi(s[start:i])
	return n
}

// atofC behaves like C's atof: parse a leading floating-point prefix,
// ignore trailing garbage, and return 0 if nothing parses.
func atofC(s string) float64 {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	sawDigit := false
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		sawDigit = true
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			sawDigit = true
		}
	}
	if !sawDigit {
		return 0
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		expStart := j
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j > expStart {
			i = j
		}
	}
	f, _ := strconv.ParseFloat(s[start:i], 64)
	return f
}

// globalNextY is the shared row counter workers claim rows from, mirroring
// the C reference's atomic_int global_next_y.
var globalNextY atomic.Int64

// computeWorker claims rows (ChunkSize at a time) from globalNextY until
// the image is fully covered, writing its assigned rows directly into
// the shared result buffer. Rows are disjoint and claimed exactly once,
// so no synchronization is needed beyond the atomic counter itself -
// mirrors the C reference's thread_mandelbrot.
func computeWorker(cfg *Config, buffer []int, wg *sync.WaitGroup) {
	defer wg.Done()
	fwidth := cfg.URx - cfg.LLx
	fheight := cfg.URy - cfg.LLy

	for {
		yStart := int(globalNextY.Add(ChunkSize) - ChunkSize)
		if yStart >= cfg.Height {
			return
		}
		yEnd := yStart + ChunkSize
		if yEnd > cfg.Height {
			yEnd = cfg.Height
		}
		for y := yStart; y < yEnd; y++ {
			for x := 0; x < cfg.Width; x++ {
				real := cfg.LLx + float64(x)*fwidth/float64(cfg.Width)
				imag := cfg.URy - float64(y)*fheight/float64(cfg.Height)
				buffer[y*cfg.Width+x] = escapeTime(real, imag, cfg.MaxIter)
			}
		}
	}
}

// finalOutput writes the completed result buffer to w, mirroring the C
// reference's final_output (single-threaded, run after all workers join).
func finalOutput(cfg *Config, buffer []int, w *bufio.Writer) {
	var itoaBuf [4]byte
	if cfg.PNG {
		for y := cfg.Height - 1; y >= 0; y-- {
			rowStart := y * cfg.Width
			for x := 0; x < cfg.Width; x++ {
				if x > 0 {
					w.WriteString(", ")
				}
				w.Write(strconv.AppendInt(itoaBuf[:0], int64(buffer[rowStart+x]), 10))
			}
			w.WriteByte('\n')
		}
	} else {
		for y := 0; y < cfg.Height; y++ {
			rowStart := y * cfg.Width
			for x := 0; x < cfg.Width; x++ {
				w.WriteByte(cnt2char(buffer[rowStart+x], cfg.MaxIter))
			}
			w.WriteByte('\n')
		}
	}
}

func main() {
	cfg := Config{
		Width:   100,
		Height:  75,
		PNG:     false,
		LLx:     -1.2,
		LLy:     0.20,
		URx:     -1.0,
		URy:     0.35,
		MaxIter: 255,
	}

	for _, arg := range os.Args[1:] {
		parseArg(arg, &cfg)
	}

	buffer := make([]int, cfg.Width*cfg.Height)

	var wg sync.WaitGroup
	wg.Add(NumThreads)
	for i := 0; i < NumThreads; i++ {
		go computeWorker(&cfg, buffer, &wg)
	}
	wg.Wait()

	w := bufio.NewWriterSize(os.Stdout, 1<<20)
	defer w.Flush()
	finalOutput(&cfg, buffer, w)
}
