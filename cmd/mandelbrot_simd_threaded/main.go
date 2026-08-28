//go:build goexperiment.simd

// Command mandelbrot_simd_threaded generates Mandelbrot set visualizations
// in ASCII or for gnuplot, using SIMD (via Go 1.27's experimental "simd"
// package) and goroutines with dynamic work-stealing (atomic row counter).
// This is a port of the C reference's mandelbrot_simd_pthread_v8.c.
//
// UNVERIFIED: written against the documented go1.27.0 "simd" package API
// but not yet compiled or run against a real Go 1.27 toolchain - I did not
// have one available while writing it. Treat this as a first draft to
// build and check, not a validated port like its scalar/threaded siblings.
//
// Like the C reference, the SIMD kernel uses float32 instead of the scalar
// path's float64. That halves precision in exchange for roughly double the
// lane count, and the C reference notes this gives ~1% of pixels an
// iteration count off by more than one compared to the scalar/double
// result - a tradeoff, not a bug, and this port inherits it. So don't
// expect byte-identical output against mandelbrot.go/mandelbrot_threaded.go
// the way those two match each other.
//
// The "simd" package is experimental and only compiled in when built with
// GOEXPERIMENT=simd (hence the build constraint above) on Go 1.27+.
//
// Build:
//
//	GOEXPERIMENT=simd go build -o mandelbrot_simd_threaded ./cmd/mandelbrot_simd_threaded
//
// Usage:
//
//	./mandelbrot_simd_threaded
//	./mandelbrot_simd_threaded width=120 ll_x=-0.75 ll_y=0.1 ur_x=-0.74 ur_y=0.11
//	./mandelbrot_simd_threaded png=1 width=800 height=600 > mandelbrot.dat
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"simd"
)

// NumThreads matches the C reference's fixed worker-pool size.
const NumThreads = 10

// ChunkSize is the number of rows claimed per atomic fetch-add - matches
// the C reference's CHUNK_SIZE (1 row per claim).
const ChunkSize = 1

// maxLanes is generous headroom for the scratch arrays below: the widest
// current hardware vector is AVX-512 at 512 bits, i.e. 16 float32/int32
// lanes. NEON (arm64) and SSE/AVX2 are narrower (4-8 lanes), so this is
// never tight, just an upper bound for stack-allocated scratch space.
const maxLanes = 16

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

// escapeTime is the scalar (float64) escape-time calculation, used for
// the remainder columns that don't fill a full SIMD lane batch - mirrors
// the C reference's use of its scalar escape_time for the same purpose.
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

// escapeTimeSIMD calculates the escape time for a full lane's worth of
// points at once, mirroring the C reference's escape_time_simd8. Every
// lane keeps iterating regardless of whether it has escaped; activeMask
// tracks which lanes are still "live" and gates how much each lane's
// iteration count advances, exactly like the C kernel's `active` vector.
//
// The simd package doesn't expose a horizontal "any lane true" reduction,
// so the early-exit check (all lanes escaped) is done by storing the
// active mask to a small scratch slice and scanning it - equivalent to
// the C kernel's `active[0]|active[1]|...|active[7]` OR-reduction, just
// spelled differently given the available API.
func escapeTimeSIMD(cr, ci simd.Float32s, maxIter int, activeScratch []int32) simd.Int32s {
	var zr, zi simd.Float32s
	var itersI simd.Int32s
	four := simd.BroadcastFloat32s(4.0)
	ones := simd.BroadcastInt32s(1)

	// Tautological comparison to get an all-true mask with no dedicated
	// constructor in the API: 0.0 == 0.0 in every lane.
	var zeroF simd.Float32s
	activeMask := zeroF.Equal(zeroF)

	for i := 0; i < maxIter; i++ {
		zr2 := zr.Mul(zr)
		zi2 := zi.Mul(zi)
		mag := zr2.Add(zi2)
		stillMask := mag.LessEqual(four)
		activeMask = activeMask.And(stillMask)

		// Early-exit test only needs "any lane nonzero", so it doesn't
		// matter whether ToInt32s represents true as -1 or 1 - either
		// way an escaped-everywhere lane set stores as all zero.
		activeMask.ToInt32s().Store(activeScratch)
		anyActive := false
		for _, v := range activeScratch {
			if v != 0 {
				anyActive = true
				break
			}
		}
		if !anyActive {
			break
		}

		twoZr := zr.Add(zr)
		newZi := twoZr.MulAdd(zi, ci) // twoZr*zi + ci
		newZr := zr2.Sub(zi2).Add(cr)
		zr, zi = newZr, newZi

		// Add 1 to active lanes, 0 to inactive ones. Masked() is defined
		// directly in terms of the mask's truthiness, so - unlike the C
		// kernel's `iters -= active` bit-pattern trick - this doesn't
		// depend on assuming how Mask32s is represented internally.
		itersI = itersI.Add(ones.Masked(activeMask))
	}

	maxIterV := simd.BroadcastInt32s(int32(maxIter))
	return maxIterV.Sub(itersI)
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
// the image is fully covered, computing each row a full SIMD lane at a
// time with a scalar remainder for the last few columns - mirrors the C
// reference's thread_mandelbrot.
func computeWorker(cfg *Config, buffer []int, wg *sync.WaitGroup) {
	defer wg.Done()
	fwidth := cfg.URx - cfg.LLx
	fheight := cfg.URy - cfg.LLy

	var probe simd.Float32s
	vecLen := probe.Len()

	var crArr [maxLanes]float32
	crBuf := crArr[:vecLen]
	var outArr [maxLanes]int32
	outBuf := outArr[:vecLen]
	var activeArr [maxLanes]int32
	activeScratch := activeArr[:vecLen]

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
			imag := cfg.URy - float64(y)*fheight/float64(cfg.Height)
			ciVec := simd.BroadcastFloat32s(float32(imag))
			rowStart := y * cfg.Width

			x := 0
			for ; x+vecLen <= cfg.Width; x += vecLen {
				for lane := 0; lane < vecLen; lane++ {
					crBuf[lane] = float32(cfg.LLx + float64(x+lane)*fwidth/float64(cfg.Width))
				}
				crVec := simd.LoadFloat32s(crBuf)
				itersVec := escapeTimeSIMD(crVec, ciVec, cfg.MaxIter, activeScratch)
				itersVec.Store(outBuf)
				for lane := 0; lane < vecLen; lane++ {
					buffer[rowStart+x+lane] = int(outBuf[lane])
				}
			}

			// Remainder columns that don't fill a full lane.
			for ; x < cfg.Width; x++ {
				real := cfg.LLx + float64(x)*fwidth/float64(cfg.Width)
				buffer[rowStart+x] = escapeTime(real, imag, cfg.MaxIter)
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
