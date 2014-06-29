// Copyright 2013 Benoît Amiaux. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package rez

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"reflect"
	"runtime"
	"testing"
)

type Tester interface {
	Fatalf(format string, args ...interface{})
}

func expect(t Tester, a, b interface{}) {
	if reflect.DeepEqual(a, b) {
		return
	}
	typea := reflect.TypeOf(a)
	typeb := reflect.TypeOf(b)
	_, file, line, _ := runtime.Caller(1)
	t.Fatalf("%v:%v got %v(%v), want %v(%v)\n", file, line,
		typea, a, typeb, b)
}

func readImage(t Tester, name string) image.Image {
	file, err := os.Open(name)
	expect(t, err, nil)
	defer file.Close()
	raw, _, err := image.Decode(file)
	expect(t, err, nil)
	return raw
}

func writeImage(t Tester, name string, img image.Image) {
	file, err := os.Create(name)
	expect(t, err, nil)
	defer file.Close()
	err = png.Encode(file, img)
	expect(t, err, nil)
}

func prepare(t Tester, dst, src image.Image, interlaced bool, filter Filter) Converter {
	cfg, err := PrepareConversion(dst, src)
	cfg.Input.Interlaced = interlaced
	cfg.Output.Interlaced = interlaced
	converter, err := NewConverter(cfg, filter)
	expect(t, err, nil)
	return converter
}

func convert(t Tester, dst, src image.Image, interlaced bool, filter Filter) {
	converter := prepare(t, dst, src, interlaced, filter)
	err := converter.Convert(dst, src)
	expect(t, err, nil)
}

func convertFiles(t Tester, w, h int, input string, filter Filter, rgb bool) (image.Image, image.Image) {
	src := readImage(t, input)
	raw := image.NewYCbCr(image.Rect(0, 0, w*2, h*2), image.YCbCrSubsampleRatio420)
	dst := raw.SubImage(image.Rect(7, 7, 7+w, 7+h))
	if rgb {
		src = toRgb(src)
		dst = toRgb(dst)
	}
	err := Convert(dst, src, filter)
	expect(t, err, nil)
	return src, dst
}

var (
	filters = []Filter{
		NewBilinearFilter(),
		NewBicubicFilter(),
		NewLanczosFilter(3),
	}
)

func TestU8(t *testing.T) {
	expect(t, u8(-1), byte(0))
	expect(t, u8(0), byte(0))
	expect(t, u8(255), byte(255))
	expect(t, u8(256), byte(255))
}

func toRgb(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, b, src, image.ZP, draw.Src)
	return dst
}

func testConvertWith(t *testing.T, rgb bool) {
	t.Skip("skipping slow test")
	sizes := []struct{ w, h int }{
		{128, 128},
		{256, 256},
		{720, 576},
		{1920, 1080},
	}
	suffix := "yuv"
	if rgb {
		suffix = "rgb"
	}
	for _, f := range filters {
		for _, s := range sizes {
			_, out := convertFiles(t, s.w, s.h, "testdata/lenna.jpg", f, rgb)
			dst := fmt.Sprintf("testdata/output-%vx%v-%v-%v.png", s.w, s.h, f.Name(), suffix)
			writeImage(t, dst, out)
		}
	}
}

func TestConvertYuv(t *testing.T) { testConvertWith(t, false) }
func TestConvertRgb(t *testing.T) { testConvertWith(t, true) }

func expectPsnrs(t *testing.T, psnrs []float64, y, uv float64) {
	for i, v := range psnrs {
		min := float64(y)
		if i > 0 {
			min = uv
		}
		expect(t, v > min, true)
	}
}

func testBoundariesWith(t *testing.T, interlaced, rgb bool) {
	// test we don't go overread/overwrite even with exotic resolutions
	src := readImage(t, "testdata/lenna.jpg")
	min := 0
	if interlaced {
		min = 1
	}
	for _, f := range filters {
		tmp := image.Image(image.NewYCbCr(image.Rect(0, 0, 256, 256), image.YCbCrSubsampleRatio444))
		convert(t, tmp, src, interlaced, f)
		last := tmp.Bounds().Dx()
		if rgb {
			tmp = toRgb(tmp)
		}
		for i := 32; i > min; i >>= 1 {
			last += i
			dst := image.Image(image.NewYCbCr(image.Rect(0, 0, last, last), image.YCbCrSubsampleRatio444))
			if rgb {
				dst = toRgb(dst)
			}
			convert(t, dst, tmp, interlaced, f)
			convert(t, tmp, dst, interlaced, f)
		}
		input := src
		final := image.Image(image.NewYCbCr(src.Bounds(), image.YCbCrSubsampleRatio420))
		if rgb {
			input = toRgb(src)
			final = toRgb(final)
		}
		convert(t, final, tmp, interlaced, f)
		if false {
			suffix := "yuv"
			if rgb {
				suffix = "rgb"
			}
			name := fmt.Sprintf("testdata/output-%v-%v-%v.png", toInterlacedString(interlaced), f.Name(), suffix)
			writeImage(t, name, final)
		}
		psnrs, err := Psnr(input, final)
		expect(t, err, nil)
		expectPsnrs(t, psnrs, 25, 38)
	}
}

func TestProgressiveYuvBoundaries(t *testing.T) { testBoundariesWith(t, false, false) }
func TestInterlacedYuvBoundaries(t *testing.T)  { testBoundariesWith(t, true, false) }
func TestProgressiveRgbBoundaries(t *testing.T) { testBoundariesWith(t, false, true) }
func TestInterlacedRgbBoundaries(t *testing.T)  { testBoundariesWith(t, true, true) }

func TestCopy(t *testing.T) {
	a, b := convertFiles(t, 512, 512, "testdata/lenna.jpg", NewBilinearFilter(), false)
	if false {
		writeImage(t, "testdata/copy-yuv.png", b)
	}
	psnrs, err := Psnr(a, b)
	expect(t, err, nil)
	expect(t, psnrs, []float64{math.Inf(1), math.Inf(1), math.Inf(1)})
	a, b = convertFiles(t, 512, 512, "testdata/lenna.jpg", NewBilinearFilter(), true)
	if false {
		writeImage(t, "testdata/copy-rgb.png", b)
	}
	psnrs, err = Psnr(a, b)
	expect(t, err, nil)
	expect(t, psnrs, []float64{math.Inf(1)})
}

func testInterlacedFailWith(t *testing.T, rgb bool) {
	src := readImage(t, "testdata/lenna.jpg")
	dst := image.Image(image.NewYCbCr(image.Rect(0, 0, 640, 480), image.YCbCrSubsampleRatio420))
	if rgb {
		src = toRgb(src)
		dst = toRgb(dst)
	}
	convert(t, dst, src, true, NewBicubicFilter())
}

func TestInterlacedFail(t *testing.T) {
	testInterlacedFailWith(t, false)
	testInterlacedFailWith(t, true)
}

func testDegradation(t *testing.T, w, h int, interlaced, rgb bool, filter Filter) {
	src := readImage(t, "testdata/lenna.jpg")
	ydst := image.NewYCbCr(image.Rect(0, 0, w*2, h*2), image.YCbCrSubsampleRatio444)
	dst := ydst.SubImage(image.Rect(7, 7, 7+w, 7+h))
	if rgb {
		src = toRgb(src)
		dst = toRgb(dst)
	}
	fwd := prepare(t, dst, src, interlaced, filter)
	bwd := prepare(t, src, dst, interlaced, filter)
	for i := 0; i < 32; i++ {
		err := fwd.Convert(dst, src)
		expect(t, err, nil)
		err = bwd.Convert(src, dst)
		expect(t, err, nil)
	}
	ref := readImage(t, "testdata/lenna.jpg")
	suffix := "yuv"
	if rgb {
		ref = toRgb(ref)
		suffix = "rgb"
	}
	psnrs, err := Psnr(ref, src)
	expect(t, err, nil)
	if false {
		name := fmt.Sprintf("testdata/degraded-%vx%v-%v-%v-%v.png", w, h, toInterlacedString(interlaced), filter.Name(), suffix)
		writeImage(t, name, src)
	}
	expectPsnrs(t, psnrs, 22, 30)
}

func TestDegradations(t *testing.T) {
	for _, f := range filters {
		testDegradation(t, 256+1, 256+1, false, false, f)
		testDegradation(t, 256+2, 256+2, true, false, f)
		if false { //too slow for now
			testDegradation(t, 256+1, 256+1, false, true, f)
			testDegradation(t, 256+2, 256+2, true, true, f)
		}
	}
}
