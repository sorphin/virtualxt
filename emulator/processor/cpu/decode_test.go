/*
Copyright (c) 2019-2020 Andreas T Jonsson

This software is provided 'as-is', without any express or implied
warranty. In no event will the authors be held liable for any damages
arising from the use of this software.

Permission is granted to anyone to use this software for any purpose,
including commercial applications, and to alter it and redistribute it
freely, subject to the following restrictions:

1. The origin of this software must not be misrepresented; you must not
   claim that you wrote the original software. If you use this software
   in a product, an acknowledgment in the product documentation would be
   appreciated but is not required.
2. Altered source versions must be plainly marked as such, and must not be
   misrepresented as being the original software.
3. This notice may not be removed or altered from any source distribution.
*/

package cpu

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/andreas-jonsson/virtualxt/emulator/memory"
	"github.com/andreas-jonsson/virtualxt/emulator/peripheral"
	"github.com/andreas-jonsson/virtualxt/emulator/peripheral/pic"
	"github.com/andreas-jonsson/virtualxt/emulator/peripheral/ram"
	"github.com/andreas-jonsson/virtualxt/emulator/peripheral/rom"
	"github.com/andreas-jonsson/virtualxt/emulator/processor"
	"github.com/andreas-jonsson/virtualxt/emulator/processor/validator"
)

func loadBin(t *testing.T, name string) []byte {
	bin, err := ioutil.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return bin
}

func runTest(t *testing.T, progName string) *CPU {
	validator.Initialize(progName+"_validator.json", validator.DefulatQueueSize, validator.DefaultBufferSize)
	defer validator.Shutdown()

	p, errs := NewCPU([]peripheral.Peripheral{
		&ram.Device{Clear: true},
		&rom.Device{
			RomName: fmt.Sprintf("TEST: %s.bin", progName),
			Base:    memory.NewPointer(0xF000, 0),
			Reader:  bytes.NewReader(loadBin(t, fmt.Sprintf("testdata/%s.bin", progName))),
		},
		&pic.Device{},
		//&debug.Device{},
	})
	defer p.Close()

	for _, err := range errs {
		t.Error(err)
	}

	//debug.EnableDebug = true

	// Tests are written for 80186+ machines.
	p.SetV20Support(true)

	p.Reset()
	p.IP = 0xFFF0
	p.CS = 0xF000

	for {
		if _, err := p.Step(); err != nil {
			if err != processor.ErrCPUHalt {
				t.Fatal(err)
			}
			break
		}
		if p.Registers.Debug {
			t.Fatal("CPU hit breakpoint!")
		}
	}
	return p
}

func runTestAndVerify(t *testing.T, progName string, nerr int) {
	p := runTest(t, progName)
	res := loadBin(t, fmt.Sprintf("testdata/res_%s.bin", progName))
	cerr := 0
	for i, v := range res {
		if r := p.ReadByte(memory.NewPointer(0, uint16(i))); r != v {
			t.Logf("Invalid result at offset 0x%X. (Got 0x%X but expected 0x%X)", i, r, v)
			cerr++
		}
	}
	if cerr != nerr {
		t.Fatalf("%d bytes diff", cerr)
	}
}

func TestAdd(t *testing.T) {
	runTestAndVerify(t, "add", 0)
}

func TestBcdcnv(t *testing.T) {
	runTestAndVerify(t, "bcdcnv", 2)
}

func TestBitwise(t *testing.T) {
	runTestAndVerify(t, "bitwise", 0)
}

func TestCmpneg(t *testing.T) {
	runTestAndVerify(t, "cmpneg", 0)
}

func TestControl(t *testing.T) {
	runTestAndVerify(t, "control", 0)
}

func TestDatatrnf(t *testing.T) {
	runTestAndVerify(t, "datatrnf", 0)
}

func TestDiv(t *testing.T) {
	runTestAndVerify(t, "div", 3)
}

func TestInterrupt(t *testing.T) {
	runTestAndVerify(t, "interrupt", 0)
}

func TestJmpmov(t *testing.T) {
	p := runTest(t, "jmpmov")
	if r := p.ReadWord(memory.Pointer(0)); r != 0x4001 {
		t.Errorf("Invalid result! (Got 0x%X but expected 0x4001)", r)
	}
}

func TestJump1(t *testing.T) {
	runTestAndVerify(t, "jump1", 0)
}

func TestJump2(t *testing.T) {
	runTestAndVerify(t, "jump2", 0)
}

func TestMul(t *testing.T) {
	runTestAndVerify(t, "mul", 8)
}

func TestRep(t *testing.T) {
	runTestAndVerify(t, "rep", 0)
}

func TestRotate(t *testing.T) {
	runTestAndVerify(t, "rotate", 0)
}

func TestSegpr(t *testing.T) {
	runTestAndVerify(t, "segpr", 0)
}

func TestShifts(t *testing.T) {
	runTestAndVerify(t, "shifts", 0)
}

func TestStrings(t *testing.T) {
	runTestAndVerify(t, "strings", 0)
}

func TestSub(t *testing.T) {
	runTestAndVerify(t, "sub", 1)
}
