set terminal pngcairo
set output 'mandelbrot.png'

# Remove margins
set lmargin at screen 0
set rmargin at screen 1 
set tmargin at screen 1 
set bmargin at screen 0

unset border
unset xtics
unset ytics

set palette defined (0 "black", 1 "white")

plot 'image.dat' matrix with image
