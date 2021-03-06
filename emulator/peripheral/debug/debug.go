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

package debug

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/andreas-jonsson/virtualxt/emulator/memory"
	"github.com/andreas-jonsson/virtualxt/emulator/peripheral"
	"github.com/andreas-jonsson/virtualxt/emulator/processor"
)

var ErrQuit = errors.New("QUIT!")

var (
	EnableDebug bool
	Stream      io.ReadWriter = &ioStream{}
	Log                       = log.New(Stream, "", 0)
)

var (
	traceInstructions,
	debugBreak bool
)

type ioStream struct {
}

func (s *ioStream) Read(p []byte) (n int, err error) {
	return os.Stdin.Read(p)
}

var magicSeq = []byte("<<<!\n")

func (s *ioStream) Write(p []byte) (n int, err error) {
	if bytes.HasSuffix(p, magicSeq) {
		n, err := os.Stdout.Write(p[:len(p)-5])
		return n + 5, err
	}
	return os.Stdout.Write(p)
}

func init() {
	flag.BoolVar(&traceInstructions, "trace", false, "Trace instruction execution")
	flag.BoolVar(&EnableDebug, "debug", false, "Enable debugger")
	flag.BoolVar(&debugBreak, "break", false, "Break on startup")
}

func readLine() string {
	scanner := bufio.NewScanner(Stream)
	for scanner.Scan() {
		return scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		log.Print(err)
	}
	return ""
}

func MuteLogging(b bool) {
	if b {
		log.SetOutput(ioutil.Discard)
		return
	}
	log.SetOutput(Stream)

	// TODO: Is this a bug? We should not need to set this.
	log.SetFlags(0)
}

type Device struct {
	signChan            chan os.Signal
	historyChan         chan string
	numInstructionsLost uint64
	lastInstruction     memory.Pointer
	breakOnIRET         bool

	mips        float64
	stats       processor.Stats
	updateStats time.Time
	breakpoints []uint16
	codeOffset  uint16

	memPeripherals [0x100000]memory.Memory

	r *processor.Registers
	p processor.Processor
}

func (m *Device) Install(p processor.Processor) error {
	m.historyChan = make(chan string, 128)
	m.signChan = make(chan os.Signal, 1)
	signal.Notify(m.signChan, os.Interrupt)

	for i := 0; i < 0x100000; i++ {
		m.memPeripherals[i] = p.GetMappedMemoryDevice(memory.Pointer(i))
	}
	if err := p.InstallMemoryDevice(m, 0x0, 0xFFFFF); err != nil {
		return err
	}

	m.p = p
	m.r = p.GetRegisters()
	m.updateStats = time.Now()
	return nil
}

func (m *Device) getFlags() string {
	s := [9]rune{'-', '-', '-', '-', '-', '-', '-', '-', '-'}
	if m.r.CF {
		s[0] = 'C'
	}
	if m.r.PF {
		s[1] = 'P'
	}
	if m.r.AF {
		s[2] = 'A'
	}
	if m.r.ZF {
		s[3] = 'Z'
	}
	if m.r.SF {
		s[4] = 'S'
	}
	if m.r.TF {
		s[5] = 'T'
	}
	if m.r.IF {
		s[6] = 'I'
	}
	if m.r.DF {
		s[7] = 'D'
	}
	if m.r.OF {
		s[8] = 'O'
	}
	return "\n" + string(s[:]) + "\n"
}

func (m *Device) printRegisters() {
	r := m.r
	regs := fmt.Sprintf(
		"AL 0x%X (%d)\tCL 0x%X (%d)\tDL 0x%X (%d)\tBL 0x%X (%d)\nAH 0x%X (%d)\tCH 0x%X (%d)\tDH 0x%X (%d)\tBH 0x%X (%d)\nAX 0x%X (%d)\tCX 0x%X (%d)\tDX 0x%X (%d)\tBX 0x%X (%d)\n\n",
		r.AL(), r.AL(), r.CL(), r.CL(), r.DL(), r.DL(), r.BL(), r.BL(),
		r.AH(), r.AH(), r.CH(), r.CH(), r.DH(), r.DH(), r.BH(), r.BH(),
		r.AX, r.AX, r.CX, r.CX, r.DX, r.DX, r.BX, r.BX,
	) + fmt.Sprintf(
		"SP 0x%X (%d)\tBP 0x%X (%d)\nSI 0x%X (%d)\tDI 0x%X (%d)\n\n",
		r.SP, r.SP, r.BP, r.BP, r.SI, r.SI, r.DI, r.DI,
	) + fmt.Sprintf(
		"ES 0x%X (%d)\tCS 0x%X (%d)\nSS 0x%X (%d)\tDS 0x%X (%d)",
		r.ES, r.ES, r.CS, r.CS, r.SS, r.SS, r.DS, r.DS,
	)
	log.Println(regs)
	log.Println(m.getFlags())
}

func instructionToString(op byte) string {
	return fmt.Sprintf("%s (0x%X)", OpcodeName(op), op)
}

func (m *Device) showMemory(rng string) {
	var from, to int
	switch n, _ := fmt.Sscanf(rng, "%x,%x", &from, &to); n {
	case 1:
		d := m.p.ReadByte(memory.Pointer(from))
		log.Printf("0x%X: 0x%X (%d)\n", from, d, d)
	case 2:
		if num := (to + 1) - from; num > 0 {
			buffer := make([]byte, num)
			for i := range buffer {
				buffer[i] = m.p.ReadByte(memory.Pointer(from + i))
			}
			log.Print(hex.Dump(buffer))
		}
	default:
		log.Println("invalid memory range")
	}
}

func toASCII(b byte) string {
	if b == 0 {
		return "."
	} else if b < 0x20 {
		return fmt.Sprint("?")
	} else if b > 0x7E {
		return fmt.Sprint("#")
	}
	return fmt.Sprintf("%c", b)
}

func (m *Device) renderVideo() {
	p := memory.Pointer(0xB8000) // Assume CGA!
	for i := 0; i < 25*2; i += 2 {
		log.Print("| <<<!")
		for j := 0; j < 80*2; j += 2 {
			log.Print(toASCII(m.p.ReadByte(p)) + "<<<!")
			p += 2
		}
		log.Print("")
	}
}

func (m *Device) setCodeOffset(of string) {
	var o uint16
	if n, _ := fmt.Sscanf(of, "%x", &o); n == 1 {
		log.Printf("Code offset at: 0x%X\n", o)
		m.codeOffset = uint16(o)
	}
}

func (m *Device) showBreakpoints() {
	for i, br := range m.breakpoints {
		log.Printf("%d:\t0x%X\n", i, br)
	}
}

func (m *Device) setBreakpoint(br string) {
	var b uint16
	if n, _ := fmt.Sscanf(br, "%x", &b); n == 1 {
		log.Printf("Breakpoint set at: CS:0x%X\n", b)
		m.breakpoints = append(m.breakpoints, b)
	}
}

func (m *Device) removeBreakpoint(br string) {
	var i int
	if n, _ := fmt.Sscanf(br, "%d", &i); n == 1 && i < len(m.breakpoints) {
		log.Printf("Removed breakpoint %d at: CS:0x%X\n", i, m.breakpoints[i])
		m.breakpoints = append(m.breakpoints[:i], m.breakpoints[i+1:]...)
	}
}

func (m *Device) showHistoryWithLength(hl string) {
	var num int
	if n, _ := fmt.Sscanf(hl, "%d", &num); n == 1 {
		if num <= 0 {
			num = 0xFFFFF
		}
		m.showHistory(num)
		return
	}
	log.Println("invalid history range")
}

func (m *Device) showHistory(num int) {
	log.Println("| Lost instructions:", m.numInstructionsLost)
	for i := 0; i < len(m.historyChan) && i < num; i++ {
		select {
		case inst := <-m.historyChan:
			log.Println(inst)
			m.historyChan <- inst
		}
	}
}

func (m *Device) pushHistory(inst string) {
	select {
	case m.historyChan <- inst:
	default:
		<-m.historyChan
		m.numInstructionsLost++
		m.historyChan <- inst
	}
}

func (m *Device) csToString() string {
	switch m.r.CS {
	case 0xF000:
		return "BIOS"
	case 0x7C00:
		return "BOOT"
	default:
		return fmt.Sprintf("0x%X", m.r.CS)
	}
}

func (m *Device) showMemMap() {
	var (
		startAddr      int
		lastDeviceName string
	)

	for i := 0; i < 0x100000; i++ {
		p, b := m.memPeripherals[i].(peripheral.Peripheral)
		name := "UNNAMED DEVICE"
		if b {
			name = p.Name()
		}

		isLast := i == 0xFFFFF
		if (lastDeviceName != name || isLast) && i > 0 {
			if end := i - 1; startAddr == end {
				log.Printf("0x%X: %s", startAddr, lastDeviceName)
			} else {
				if isLast {
					end++
				}
				log.Printf("0x%X-0x%X: %s", startAddr, end, lastDeviceName)
			}
			startAddr = i
		}
		lastDeviceName = name
	}
}

func (m *Device) ReadByte(addr memory.Pointer) byte {
	return m.memPeripherals[addr].ReadByte(addr)
}

func (m *Device) WriteByte(addr memory.Pointer, data byte) {
	m.memPeripherals[addr].WriteByte(addr, data)
	if data != 0 && addr == memory.NewPointer(0x40, 0x15) {
		log.Printf("BIOS Error: 0x%X", data)
		m.Break()
	}
	/*
		switch addr {
		case 0x70:
			log.Printf("Write: 0x%X @ %v", data, addr)
			m.Break()
		}
	*/
}

func (m *Device) Break() {
	debugBreak = true
	m.r.Debug = true
}

func (m *Device) Continue() {
	debugBreak = false
	m.r.Debug = false
}

func (m *Device) Step(cycles int) error {
	if time.Since(m.updateStats) >= time.Second {
		m.stats = m.p.GetStats()
		m.mips = float64(m.stats.NumInstructions) / 1000000.0
		m.updateStats = time.Now()
	}

	if m.r.Debug {
		debugBreak = true
	}

	select {
	case <-m.signChan:
		log.Println("BREAK!")
		m.Break()
	default:
	}

	ip := memory.NewPointer(m.r.CS, m.r.IP)
	op := m.p.ReadByte(ip)
	inst := instructionToString(op)

	if m.lastInstruction > 0 && m.lastInstruction != ip {
		m.Break()
		m.lastInstruction = 0
		log.Println(inst)
	}

	if m.breakOnIRET && op == 0xCF {
		m.Break()
		m.breakOnIRET = false
		log.Println(inst)
	}

	for i, br := range m.breakpoints {
		if m.r.IP == br {
			log.Println("BREAK:", i)
			m.Break()
		}
	}

	for debugBreak {

		log.Printf("[%s:0x%X] DEBUG><<<!", m.csToString(), m.r.IP-m.codeOffset)

		ln := readLine()
		switch {
		case ln == "q":
			return ErrQuit
		case ln == "c":
			m.Continue()
		case ln == "" || ln == "s":
			m.Continue()
			m.lastInstruction = ip
		case ln == "i":
			m.Continue()
			m.breakOnIRET = true
		case ln == "r":
			m.printRegisters()
		case ln == "v":
			m.renderVideo()
		case ln == "t":
			m.showHistory(16)
		case ln == "ct":
			log.Print("Clear trace!")
		drainHistory:
			select {
			case <-m.historyChan:
				m.numInstructionsLost++
				goto drainHistory
			default:
			}
		case ln == "t":
			log.Printf("MIPS: %.2f\n", m.mips)
			log.Print(m.stats)
		case ln == "@":
			log.Printf("%v (%d)\n", ip-memory.Pointer(m.codeOffset), ip-memory.Pointer(m.codeOffset))
			log.Print(inst)
		case ln == "cb":
			log.Print("Clear breakpoints!")
			m.breakpoints = m.breakpoints[:0]
		case ln == "b":
			m.showBreakpoints()
		case ln == "p":
			m.showMemMap()
		case strings.HasPrefix(ln, "o "):
			m.setCodeOffset(ln[2:])
		case strings.HasPrefix(ln, "t "):
			m.showHistoryWithLength(ln[2:])
		case strings.HasPrefix(ln, "b "):
			m.setBreakpoint(ln[2:])
		case strings.HasPrefix(ln, "rb "):
			m.removeBreakpoint(ln[3:])
		case strings.HasPrefix(ln, "m "):
			m.showMemory(ln[2:])
		default:
			log.Print("unknown command: ", ln)
		}
	}

	if traceInstructions {
		m.pushHistory(fmt.Sprintf("| [%s:0x%X] %s", m.csToString(), m.r.IP-m.codeOffset, inst))
	}

	return nil
}

func (m *Device) Name() string {
	return "Debug Device"
}

func (m *Device) Reset() {
}
