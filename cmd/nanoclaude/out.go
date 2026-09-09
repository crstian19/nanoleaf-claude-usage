package main

import (
	"fmt"
	"io"
)

// out is an error-accumulating writer -- the errWriter pattern from Rob Pike's
// "Errors are values", for code that emits many lines in a row.
//
// A command printing a table would otherwise have to check an error after
// every single Fprintf, which buries the formatting in noise; here the first
// failure is remembered and reported once at the end.
type out struct {
	w   io.Writer
	err error
}

func newOut(w io.Writer) *out { return &out{w: w} }

// printf writes a formatted line, doing nothing once a write has failed.
func (o *out) printf(format string, args ...any) {
	if o.err != nil {
		return
	}
	_, o.err = fmt.Fprintf(o.w, format, args...)
}

// print writes a string verbatim.
func (o *out) print(s string) {
	if o.err != nil {
		return
	}
	_, o.err = io.WriteString(o.w, s)
}

// Err reports the first write failure, if any.
func (o *out) Err() error { return o.err }
