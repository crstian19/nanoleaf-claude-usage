// Package webui serves the local page that turns the panel shape until it
// matches the wall.
//
// The page exists because orienting a wall of panels is a visual job. The
// device knows where its panels are relative to each other, but nothing tells
// it which way is up in the room, and the first two attempts at asking the
// user both failed: naming an angle in degrees is not something a person can
// do, and a terminal dial can only draw the shape as coloured blocks. A
// browser can draw the real triangles, and a mouse can turn them.
//
// Everything the page draws is computed here and sent to it. The browser does
// no geometry of its own beyond reading the mouse, so the picture on the
// screen and the picture on the wall come from one calculation and cannot
// disagree.
package webui
