package commands

import "math"

// colour is a light's colour without its brightness: hue in degrees (0-360)
// and saturation (0-1).
type colour struct {
	hue, sat float64
}

func rgbColour(r, g, b float64) colour {
	r, g, b = r/255, g/255, b/255
	hi := math.Max(r, math.Max(g, b))
	lo := math.Min(r, math.Min(g, b))
	if hi <= 0 {
		return colour{}
	}
	d := hi - lo
	c := colour{sat: d / hi}
	if d == 0 {
		return c
	}
	switch hi {
	case r:
		c.hue = math.Mod((g-b)/d, 6)
	case g:
		c.hue = (b-r)/d + 2
	default:
		c.hue = (r-g)/d + 4
	}
	c.hue *= 60
	if c.hue < 0 {
		c.hue += 360
	}
	return c
}

// xyColour converts CIE 1931 chromaticity to a colour. Points outside sRGB,
// such as a Zigbee bulb's deeper green, are brought into it, which keeps
// their hue.
func xyColour(x, y float64) colour {
	if y <= 0 {
		return colour{}
	}
	X, Y, Z := x/y, 1.0, (1-x-y)/y
	r := 3.2406*X - 1.5372*Y - 0.4986*Z
	g := -0.9689*X + 1.8758*Y + 0.0415*Z
	b := 0.0557*X - 0.2040*Y + 1.0570*Z
	lo := math.Min(r, math.Min(g, b))
	if lo < 0 { // out of gamut: desaturate towards white just enough
		r, g, b = r-lo, g-lo, b-lo
	}
	hi := math.Max(r, math.Max(g, b))
	if hi <= 0 {
		return colour{}
	}
	return rgbColour(255*r/hi, 255*g/hi, 255*b/hi)
}

// hsColour reads hue in degrees and saturation in percent (0-100), as
// light_set_hs and a light's hs state use.
func hsColour(h, s float64) colour {
	return colour{hue: math.Mod(math.Mod(h, 360)+360, 360), sat: s / 100}
}

// requestedColour reads the colour a colour command asks for.
func requestedColour(action string, p map[string]any) (colour, bool) {
	n := func(k string) (float64, bool) { v, ok := p[k].(float64); return v, ok }
	switch action {
	case "light_set_rgb", "light_set_rgbw", "light_set_rgbww":
		r, ok1 := n("r")
		g, ok2 := n("g")
		b, ok3 := n("b")
		return rgbColour(r, g, b), ok1 && ok2 && ok3
	case "light_set_hs":
		h, ok1 := n("hue")
		s, ok2 := n("saturation")
		return hsColour(h, s), ok1 && ok2
	case "light_set_xy":
		x, ok1 := n("x")
		y, ok2 := n("y")
		return xyColour(x, y), ok1 && ok2
	}
	return colour{}, false
}

// reportedColour reads a light's colour from its state, in whichever form the
// light reports it: the form its colorMode names first, then rgb, hs or xy.
// A light in a white mode has no colour.
func reportedColour(state map[string]any) (colour, bool) {
	nums := func(k string, n int) ([]float64, bool) {
		raw, ok := state[k].([]any)
		if !ok || len(raw) < n {
			return nil, false
		}
		out := make([]float64, n)
		for i := range out {
			v, ok := raw[i].(float64)
			if !ok {
				return nil, false
			}
			out[i] = v
		}
		return out, true
	}
	from := map[string]func() (colour, bool){
		"rgb": func() (colour, bool) {
			v, ok := nums("rgb", 3)
			if !ok {
				return colour{}, false
			}
			return rgbColour(v[0], v[1], v[2]), true
		},
		"hs": func() (colour, bool) {
			v, ok := nums("hs", 2)
			if !ok {
				return colour{}, false
			}
			return hsColour(v[0], v[1]), true
		},
		"xy": func() (colour, bool) {
			v, ok := nums("xy", 2)
			if !ok {
				return colour{}, false
			}
			return xyColour(v[0], v[1]), true
		},
	}
	mode, _ := state["colorMode"].(string)
	switch mode {
	case "color_temp", "cold_warm_white", "white", "brightness", "onoff":
		return colour{}, false
	}
	if f, ok := from[mode]; ok {
		if c, ok := f(); ok {
			return c, true
		}
	}
	for _, k := range []string{"rgb", "hs", "xy"} {
		if c, ok := from[k](); ok {
			return c, true
		}
	}
	return colour{}, false
}

// sameColour reports whether a light shows the colour asked for. Bulbs map a
// colour into their own range, so hue is compared loosely and saturation only
// roughly: what matters is green looking green, and white looking white.
func sameColour(want, got colour) bool {
	if want.sat < 0.2 {
		return got.sat < 0.35
	}
	if got.sat < want.sat*0.5 {
		return false
	}
	d := math.Abs(want.hue - got.hue)
	return math.Min(d, 360-d) <= 30
}
