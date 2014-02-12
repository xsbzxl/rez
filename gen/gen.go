// Copyright 2014 Benoît Amiaux. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	. "github.com/bamiaux/rez/asm"
	"log"
	"os"
)

type context struct {
	xtaps  int
	xshift uint
	// global data
	zero  Operand
	hbits Operand
	// arguments
	dst    []Operand
	src    []Operand
	cof    []Operand
	off    []Operand
	taps   Operand
	width  Operand
	height Operand
	dp     Operand
	sp     Operand
	// stack
	simdroll Operand
	asmroll  Operand
	srcref   Operand
	dstoff   Operand
	sum      Operand
	dstref   Operand
	count    Operand
	inner    Operand
}

func main() {
	a := NewAsm(os.Stdout)
	c := context{xshift: 4}
	c.zero = a.Data("zero", bytes.Repeat([]byte{0x00}, 16))
	c.hbits = a.Data("hbits", bytes.Repeat([]byte{0x00, 0x00, 0x20, 0x00}, 4))
	genh8scale(a, &c, 2)
	genh8scale(a, &c, 4)
	genh8scale(a, &c, 8)
	genh8scale(a, &c, 10)
	genh8scale(a, &c, 12)
	genh8scale(a, &c, 0)
	err := a.Flush()
	if err != nil {
		log.Fatalln(err)
	}
}

func genh8scale(a *Asm, c *context, taps int) {
	c.xtaps = taps
	suffix := "N"
	if taps > 0 {
		suffix = fmt.Sprintf("%v", taps)
	}
	a.NewFunction("h8scale" + suffix)
	// arguments
	c.dst = a.SliceArgument("dst")
	c.src = a.SliceArgument("src")
	c.cof = a.SliceArgument("cof")
	c.off = a.SliceArgument("off")
	c.taps = a.Argument("taps")
	c.width = a.Argument("width")
	c.height = a.Argument("height")
	c.dp = a.Argument("dp")
	c.sp = a.Argument("sp")
	// stack
	c.simdroll = a.PushStack("simdroll")
	c.asmroll = a.PushStack("asmroll")
	c.srcref = a.PushStack("srcref")
	c.dstoff = a.PushStack("dstoff")
	c.sum = a.PushStack("sum")
	if c.xtaps == 0 {
		c.dstref = a.PushStack("dstref")
		c.count = a.PushStack("count")
		c.inner = a.PushStack("inner")
	}
	a.Start()
	frame(a, c)
	a.Ret()
}

func setup(a *Asm, c *context) {
	a.Movq(BX, c.dp)
	a.Movq(CX, c.width)
	a.Movq(DX, CX)
	a.Subq(BX, CX)
	a.Shrq(CX, Constant(c.xshift))
	a.Andq(DX, Constant(1<<c.xshift-1))
	a.Movq(c.dstoff, BX)
	a.Movq(c.simdroll, CX)
	a.Movq(c.asmroll, DX)
	if false {
		// disable simd loops
		a.Movq(AX, Constant(0))
		a.Movq(c.simdroll, AX)
		a.Movq(AX, c.width)
		a.Movq(c.asmroll, AX)
	}
	a.Movq(AX, c.src[0])
	a.Movq(c.srcref, AX)
	a.Movq(DX, c.taps)
	a.Subq(DX, Constant(2))
	if c.xtaps == 0 {
		a.Movq(c.inner, DX)
	}
	a.Pxor(X15, X15)
	a.Movo(X14, c.hbits)
}

func frame(a *Asm, c *context) {
	setup(a, c)
	a.Movq(SI, c.src[0])
	a.Movq(DI, c.dst[0])
	yloop := a.NewLabel("yloop")
	a.Label(yloop)
	a.Movq(BX, c.off[0])
	a.Movq(BP, c.cof[0])
	line(a, c)
	nextline(a, c)
	a.Subq(c.height, Constant(1))
	a.Jne(yloop)
}

func nextline(a *Asm, c *context) {
	a.Movq(SI, c.srcref)
	a.Addq(DI, c.dstoff)
	a.Addq(SI, c.sp)
	a.Movq(c.srcref, SI)
}

func line(a *Asm, c *context) {
	simdloop := a.NewLabel("simdloop")
	asmloop := a.NewLabel("asmloop")
	nosimdloop := a.NewLabel("nosimdloop")
	end := a.NewLabel("end")

	// check if we have simd loops
	a.Movq(CX, c.simdroll)
	a.Orq(CX, CX)
	a.Je(nosimdloop)

	// apply simd loops
	a.Label(simdloop)
	switch c.xtaps {
	case 2:
		htaps2(a, c)
	case 4:
		htaps4(a, c)
	case 8:
		htaps8(a, c)
	case 10, 12, 0:
		htapsn(a, c)
	}
	a.Subq(CX, Constant(1))
	a.Jne(simdloop)

	// check if we have asm loops
	a.Label(nosimdloop)
	a.Movq(CX, c.asmroll)
	a.Orq(CX, CX)
	a.Je(end)

	// apply asm loops
	a.Label(asmloop)
	asmhtaps(a, c)
	a.Subq(CX, Constant(1))
	a.Jne(asmloop)

	a.Label(end)
}

func htaps1(a *Asm, c *context, idx int) {
	a.Movq(DX, Address(BX))
	a.Movbqzx(AX, Address(SI, DX, idx))
	a.Movwqsx(DX, Address(BP, idx*2))
	a.Imulq(DX)
}

func asmhtaps(a *Asm, c *context) {
	htaps1(a, c, 0)
	a.Movq(c.sum, AX)
	if c.xtaps > 0 {
		i := 1
		for ; i <= c.xtaps-2; i++ {
			htaps1(a, c, i)
			a.Addq(c.sum, AX)
		}
		htaps1(a, c, i)
		a.Addq(BP, Constant(c.xtaps*2))
	} else {
		a.Movq(AX, c.inner)
		a.Movq(c.count, AX)
		loop := a.NewLabel("loop")
		a.Label(loop)
		htaps1(a, c, 1)
		a.Addq(SI, Constant(1))
		a.Addq(BP, Constant(2))
		a.Addq(c.sum, AX)
		a.Subq(c.count, Constant(1))
		a.Jne(loop)
		htaps1(a, c, 1)
		a.Addq(BP, Constant(2*2))
		a.Subq(SI, c.inner)
	}
	a.Addq(AX, c.sum)
	a.Addq(AX, Constant(1<<(14-1)))
	a.Cmovql(AX, c.zero)
	a.Shrq(AX, Constant(14))
	a.Addq(BX, Constant(8))
	a.Movb(Address(DI), AL)
	a.Addq(DI, Constant(1))
}

func hload2(a *Asm, op Operand, idx uint) {
	a.Movq(AX, Address(BX, (idx*4+0)*8))
	a.Movq(DX, Address(BX, (idx*4+1)*8))
	a.Pinsrw(op, Address(SI, AX), Constant(0))
	a.Pinsrw(op, Address(SI, DX), Constant(1))
	a.Movq(AX, Address(BX, (idx*4+2)*8))
	a.Movq(DX, Address(BX, (idx*4+3)*8))
	a.Pinsrw(op, Address(SI, AX), Constant(2))
	a.Pinsrw(op, Address(SI, DX), Constant(3))
}

func htaps2(a *Asm, c *context) {
	hload2(a, X0, 0)
	hload2(a, X1, 1)
	hload2(a, X2, 2)
	hload2(a, X3, 3)
	a.Punpcklbw(X0, X15)
	a.Punpcklbw(X1, X15)
	a.Punpcklbw(X2, X15)
	a.Punpcklbw(X3, X15)
	xwidth := uint(1 << c.xshift)
	a.Addq(BX, Constant(xwidth*8))
	a.Pmaddwd(X0, Address(BP, xwidth*0))
	a.Pmaddwd(X1, Address(BP, xwidth*1))
	a.Pmaddwd(X2, Address(BP, xwidth*2))
	a.Pmaddwd(X3, Address(BP, xwidth*3))
	a.Paddd(X0, X14)
	a.Paddd(X1, X14)
	a.Paddd(X2, X14)
	a.Paddd(X3, X14)
	a.Addq(BP, Constant(xwidth*4))
	a.Psrad(X0, Constant(14))
	a.Psrad(X1, Constant(14))
	a.Psrad(X2, Constant(14))
	a.Psrad(X3, Constant(14))
	a.Packssdw(X0, X1)
	a.Packssdw(X2, X3)
	a.Packuswb(X0, X2)
	a.Store(Address(DI), X0)
	a.Addq(DI, Constant(xwidth))
}

func hload4(a *Asm, xa, xb SimdRegister, idx int, tmpa, tmpb SimdRegister) {
	a.Movq(AX, Address(BX, (idx*4+0)*8))
	a.Movq(DX, Address(BX, (idx*4+1)*8))
	a.Movd(xa, Address(SI, AX))
	a.Movd(tmpa, Address(SI, DX))
	a.Movq(AX, Address(BX, (idx*4+2)*8))
	a.Movq(DX, Address(BX, (idx*4+3)*8))
	a.Movd(xb, Address(SI, AX))
	a.Movd(tmpb, Address(SI, DX))
	a.Punpckldq(xa, tmpa)
	a.Punpckldq(xb, tmpb)
}

func hmadd4(a *Asm, xwidth uint, xa, xb, xc, xd SimdRegister, idx uint, tmpa, tmpb SimdRegister) {
	a.Punpcklbw(xa, X15)
	a.Pmaddwd(xa, Address(BP, (idx*4+0)*xwidth))
	a.Punpcklbw(xb, X15)
	a.Pmaddwd(xb, Address(BP, (idx*4+1)*xwidth))
	a.Punpcklbw(xc, X15)
	a.Pmaddwd(xc, Address(BP, (idx*4+2)*xwidth))
	a.Punpcklbw(xd, X15)
	a.Pmaddwd(xd, Address(BP, (idx*4+3)*xwidth))
	a.Movo(tmpa, xa)
	a.Movo(tmpb, xc)
	a.Shufps(tmpa, xb, Constant(0xDD))
	a.Shufps(tmpb, xd, Constant(0xDD))
	a.Shufps(xa, xb, Constant(0x88))
	a.Shufps(xc, xd, Constant(0x88))
	a.Paddd(xa, tmpa)
	a.Paddd(xc, tmpb)
	a.Paddd(xa, X14)
	a.Paddd(xc, X14)
}

func htaps4(a *Asm, c *context) {
	hload4(a, X0, X2, 0, X1, X3)
	hload4(a, X4, X6, 1, X5, X7)
	xwidth := uint(1 << c.xshift)
	hmadd4(a, xwidth, X0, X2, X4, X6, 0, X1, X5)
	a.Psrad(X0, Constant(14))
	a.Psrad(X4, Constant(14))
	a.Packssdw(X0, X4)
	hload4(a, X1, X3, 2, X2, X4)
	hload4(a, X5, X7, 3, X6, X2)
	a.Addq(BX, Constant(xwidth*8))
	hmadd4(a, xwidth, X1, X3, X5, X7, 1, X4, X6)
	a.Addq(BP, Constant(xwidth*8))
	a.Psrad(X1, Constant(14))
	a.Psrad(X5, Constant(14))
	a.Packssdw(X1, X5)
	a.Packuswb(X0, X1)
	a.Store(Address(DI), X0)
	a.Addq(DI, Constant(xwidth))
}

func hload8(a *Asm, xa, xb SimdRegister, idx uint, xc, xd SimdRegister) {
	a.Movq(AX, Address(BX, (idx*4+0)*8))
	a.Hstore(xa, Address(SI, AX))
	a.Movq(DX, Address(BX, (idx*4+1)*8))
	a.Hstore(xb, Address(SI, DX))
	a.Movq(AX, Address(BX, (idx*4+2)*8))
	a.Hstore(xc, Address(SI, AX))
	a.Movq(DX, Address(BX, (idx*4+3)*8))
	a.Hstore(xd, Address(SI, DX))
}

func hpadd8(a *Asm, xa, xb, xc, xd, tmpa, tmpb SimdRegister) {
	a.Movo(tmpa, xa)
	a.Movo(tmpb, xc)
	a.Punpcklqdq(xa, xb)
	a.Punpckhqdq(tmpa, xb)
	a.Paddd(xa, tmpa)
	a.Punpcklqdq(xc, xd)
	a.Punpckhqdq(tmpb, xd)
	a.Paddd(xc, tmpb)
	a.Movo(tmpa, xa)
	a.Shufps(xa, xc, Constant(0x88))
	a.Shufps(tmpa, xc, Constant(0xDD))
	a.Paddd(xa, tmpa)
}

func hmadd8(a *Asm, xwidth uint, xa, xb, xc, xd SimdRegister, idx uint, tmpa, tmpb SimdRegister) {
	a.Punpcklbw(xa, X15)
	a.Punpcklbw(xb, X15)
	a.Punpcklbw(xc, X15)
	a.Punpcklbw(xd, X15)
	a.Pmaddwd(xa, Address(BP, (idx*4+0)*xwidth))
	a.Pmaddwd(xb, Address(BP, (idx*4+1)*xwidth))
	a.Pmaddwd(xc, Address(BP, (idx*4+2)*xwidth))
	a.Pmaddwd(xd, Address(BP, (idx*4+3)*xwidth))
	hpadd8(a, xa, xb, xc, xd, tmpa, tmpb)
	a.Paddd(xa, X14)
	a.Psrad(xa, Constant(14))
}

func htaps8(a *Asm, c *context) {
	xwidth := uint(1 << c.xshift)
	hload8(a, X0, X1, 0, X2, X3)
	hmadd8(a, xwidth, X0, X1, X2, X3, 0, X6, X7)
	hload8(a, X4, X5, 1, X6, X7)
	hmadd8(a, xwidth, X4, X5, X6, X7, 1, X1, X2)
	hload8(a, X1, X2, 2, X3, X5)
	hmadd8(a, xwidth, X1, X2, X3, X5, 2, X6, X7)
	hload8(a, X2, X3, 3, X5, X6)
	hmadd8(a, xwidth, X2, X3, X5, X6, 3, X7, X8)
	a.Addq(BX, Constant(xwidth*8))
	a.Addq(BP, Constant(xwidth*16))
	a.Packssdw(X0, X4)
	a.Packssdw(X1, X2)
	a.Packuswb(X0, X1)
	a.Store(Address(DI), X0)
	a.Addq(DI, Constant(xwidth))
}

func hloadn(a *Asm, xa, xb, xc, xd SimdRegister) {
	hload2(a, xa, 0)
	hload2(a, xb, 1)
	hload2(a, xc, 2)
	hload2(a, xd, 3)
	a.Addq(SI, Constant(2))
}

func hmaddn(a *Asm, xwidth uint, xa, xb, xc, xd SimdRegister) {
	a.Punpcklbw(xa, X15)
	a.Pmaddwd(xa, Address(BP, xwidth*0))
	a.Punpcklbw(xb, X15)
	a.Pmaddwd(xb, Address(BP, xwidth*1))
	a.Punpcklbw(xc, X15)
	a.Pmaddwd(xc, Address(BP, xwidth*2))
	a.Punpcklbw(xd, X15)
	a.Pmaddwd(xd, Address(BP, xwidth*3))
	a.Addq(BP, Constant(xwidth*4))
}

func htapsn(a *Asm, c *context) {
	xwidth := uint(1 << c.xshift)
	hloadn(a, X0, X1, X2, X3)
	hmaddn(a, xwidth, X0, X1, X2, X3)
	// unloop when we know how many taps
	for i := 1; i*2 < c.xtaps; i++ {
		hloadn(a, X4, X5, X6, X7)
		hmaddn(a, xwidth, X4, X5, X6, X7)
		a.Paddd(X0, X4)
		a.Paddd(X1, X5)
		a.Paddd(X2, X6)
		a.Paddd(X3, X7)
	}
	if c.xtaps == 0 {
		a.Movq(c.dstref, DI)
		a.Movq(DI, c.inner)
		loop := a.NewLabel("loop")
		a.Label(loop)
		hloadn(a, X4, X5, X6, X7)
		hmaddn(a, xwidth, X4, X5, X6, X7)
		a.Paddd(X0, X4)
		a.Paddd(X1, X5)
		a.Paddd(X2, X6)
		a.Paddd(X3, X7)
		a.Subq(DI, Constant(2))
		a.Jne(loop)
		a.Movq(DI, c.dstref)
	}
	a.Addq(BX, Constant(xwidth*8))
	a.Paddd(X0, X14)
	a.Paddd(X1, X14)
	a.Paddd(X2, X14)
	a.Paddd(X3, X14)
	a.Movq(AX, c.taps)
	a.Psrad(X0, Constant(14))
	a.Psrad(X1, Constant(14))
	a.Psrad(X2, Constant(14))
	a.Psrad(X3, Constant(14))
	a.Subq(SI, AX)
	a.Packssdw(X0, X1)
	a.Packssdw(X2, X3)
	a.Packuswb(X0, X2)
	a.Store(Address(DI), X0)
	a.Addq(DI, Constant(xwidth))
}