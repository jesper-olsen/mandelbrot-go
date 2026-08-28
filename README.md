# Mandelbrot in Go

This repository contains a Go implementation for generating visualizations of the Mandelbrot set.

The program compiles to a single native executable. It can render the Mandelbrot set directly to the terminal as ASCII art or produce a data file for `gnuplot` to generate a high-resolution PNG image.

### Other Language Implementations

This project is part of a suite of mandelbrot implementations in different languages.

Single Thread/Multi-thread shows the number of seconds it takes to do a 5000x5000 calculation.


| Language    | Repository                                                           | Single Thread   | Multi-Thread | Simd | Multi-Thread + Simd |
| :--------   | :------------------------------------------------------------------- | ---------------:| -----------: | ----:| ------------------: |
| Awk         | [mandelbrot-awk](https://github.com/jesper-olsen/mandelbrot-awk)     |           417.9 |              |      |                     |
| **C**       | [mandelbrot-c](https://github.com/jesper-olsen/mandelbrot-c)         |             3.6 |          0.6 |  0.7 |               0.2   |
| Erlang      | [mandelbrot_erl](https://github.com/jesper-olsen/mandelbrot_erl)     |            35.6 |          8.3 |      |                     |
| Fortran     | [mandelbrot-f](https://github.com/jesper-olsen/mandelbrot-f)         |             4.5 |              |      |                     |
| Go          | [mandelbrot-go](https://github.com/jesper-olsen/mandelbrot-go)       |             4.1 |          0.8 |  1.3 |               0.4   |
| Java        | [mandelbrot-java](https://github.com/jesper-olsen/mandelbrot-java)   |             3.9 |          0.8 |  1.4 |               0.5   |
| Lua         | [mandelbrot-lua](https://github.com/jesper-olsen/mandelbrot-lua)     |            33.2 |              |      |                     |
| Mojo        | [mandelbrot-mojo](https://github.com/jesper-olsen/mandelbrot-mojo)   |             3.8 |          1.2 |  0.7 |               0.4   |
| Nushell     | [mandelbrot-nu](https://github.com/jesper-olsen/mandelbrot-nu)       |         17186.6 |              |      |                     |
| Odin        | [mandelbrot-odin](https://github.com/jesper-olsen/mandelbrot-odin)   |             4.4 |              |      |                     |
| Python      | [mandelbrot-py](https://github.com/jesper-olsen/mandelbrot-py)       |     (pure) 93.3 | (jax)    5.9 |      |                     |
| R           | [mandelbrot-R](https://github.com/jesper-olsen/mandelbrot-R)         |           335.0 |              |      |                     |
| Rust        | [mandelbrot-rs](https://github.com/jesper-olsen/mandelbrot-rs)       |             4.7 |          1.3 |      |                     |
| Swift       | [mandelbrot-swift](https://github.com/jesper-olsen/mandelbrot-swift) |             4.5 |          1.2 |  1.3 |               0.7   |
| Tcl         | [mandelbrot-tcl](https://github.com/jesper-olsen/mandelbrot-tcl)     |           306.9 |              |      |                     |
| Zig         | [mandelbrot-zig](https://github.com/jesper-olsen/mandelbrot-zig)     |             4.9 |          0.9 |  0.7 |               0.3   |


---

## Prerequisites

You will need the following installed:

1. A **Go toolchain** (1.27 or later).
2. **Make** (optional, but recommended for easy building).
3. **Gnuplot** (required *only* for generating PNG images).

---

## Build

You can build the program directly with `go build` or use the provided Makefile.

**Option 1: Manual Compilation**

```
go build -o mandelbrot .
```

**Option 2: Using Make**

```
make
```
The `simd` package is currently experimental in go (1.27) - to also build `mandelbrot_simd_threaded` which takes advantage of both SIMD 
and threading, do:
```
make mandelbrot_simd_threaded
```

---

## Usage

The compiled executable can be configured via command-line arguments using a `key=value` format, same as the C reference implementation.

### 1. ASCII Art Output

To render the Mandelbrot set directly in your terminal, run the executable.

```
./mandelbrot
```

You can change the view and resolution by passing parameters:

```
# Zoom in on a different area with a wider view
./mandelbrot width=120 ll_x=-0.75 ll_y=0.1 ur_x=-0.74 ur_y=0.11
```

### 2. PNG Image Generation

To create a high-resolution PNG, you first generate a data file and then process it with `gnuplot`.

**Step 1: Generate the data file**

Set `png=1` and specify the desired dimensions. Redirect the output to a file.

```
./mandelbrot png=1 width=1000 height=750 > image.dat
```

**Step 2: Run gnuplot**

This will read `image.dat` and create `mandelbrot.png`.

```
gnuplot topng.gp
```

The result is a high-quality `mandelbrot.png` image.

## Performance

Benchmarks were run on an **Apple M5** system with go version go1.27.0 darwin/arm64

**Generating a 1000x750 data file:**
```sh
time ./mandelbrot png=1 width=1000 height=750 > image.dat
0.15s user 0.00s system 98% cpu 0.161 total
```

**Generating a 5000x5000 data file:**
```sh
time ./mandelbrot png=1 width=5000 height=5000 > image.dat
4.04s user 0.03s system 99% cpu 4.088 total
```

**Generating a 5000x5000 data file with multiple worker threads:**
```sh
time ./mandelbrot_threaded png=1 width=5000 height=5000 > image.dat
5.79s user 0.09s system 701% cpu 0.838 total
```

**Generating a 5000x5000 data file with SIMD:**
```sh
time ./mandelbrot_simd_threaded  png=1 width=5000 height=5000 > image.dat
1.29s user 0.04s system 99% cpu 1.346 total
```

**Generating a 5000x5000 data file with SIMD and multiple worker threads:**
```sh
time ./mandelbrot_simd_threaded  png=1 width=5000 height=5000 > image.dat
1.77s user 0.07s system 445% cpu 0.413 total
```

### Topics

[mandelbrot](https://github.com/topics/mandelbrot)
