// +build validator

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

package validator

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"math"
	"os"

	"github.com/andreas-jonsson/virtualxt/emulator/processor"
)

const Enabled = true

var outputFile string

var (
	inScope      bool
	currentEvent Event
	outputChan   chan Event
	quitChan     chan struct{}
)

func Initialize(output string, queueSize, bufferSize int) {
	if outputFile = output; output == "" {
		return
	}

	outputChan = make(chan Event, queueSize)
	quitChan = make(chan struct{})

	fp, err := os.Create(outputFile)
	if err != nil {
		log.Panic(err)
	}

	go func() {
		var buffer bytes.Buffer

		defer fp.Close()
		defer func() { io.Copy(fp, &buffer); quitChan <- struct{}{} }()

		enc := json.NewEncoder(&buffer)

		for ev := range outputChan {
			if err := enc.Encode(ev); err != nil {
				log.Print(err)
				return
			}
			if buffer.Len() >= bufferSize {
				log.Print("Flush validation events!")
				if _, err := io.Copy(fp, &buffer); err != nil {
					log.Print(err)
					return
				}
			}
		}
	}()
}

func Begin(opcode byte, regs processor.Registers) {
	if outputFile == "" {
		return
	}

	inScope = true
	currentEvent = EmptyEvent
	currentEvent.Opcode = opcode
	currentEvent.Regs[0] = regs

}

func End(regs processor.Registers) {
	if !inScope {
		return
	}

	inScope = false
	currentEvent.Regs[1] = regs
	outputChan <- currentEvent
}

func Discard() {
	inScope = false
}

func ReadByte(addr uint32, data byte) {
	if !inScope {
		return
	}
	for i, op := range currentEvent.Reads {
		if op.Addr == math.MaxUint32 {
			currentEvent.Reads[i] = MemOp{addr, data}
			return
		}
	}
	log.Panic("Max reads!")
}

func WriteByte(addr uint32, data byte) {
	if !inScope {
		return
	}
	for i, op := range currentEvent.Writes {
		if op.Addr == math.MaxUint32 {
			currentEvent.Writes[i] = MemOp{addr, data}
			return
		}
	}
	log.Panic("Max writes!")
}

func Shutdown() {
	if outputFile == "" {
		return
	}
	close(outputChan)
	<-quitChan
}
