package ca65

import (
	"fmt"
	"strings"
)

const (
	memoryConfigPart1 = `
MEMORY {
    ZP:          start = $00,    size = $100,    type = rw, file = "";
    RAM:         start = $0200,  size = $600,    type = rw, file = "";
    HDR:         start = $0000,  size = $10,     type = ro, file = %O, fill = yes;
`

	memoryPrgBankTemplate = `    %-12s start = $%04X,  size = $%04X,   type = ro, file = %%O, fill = yes;
`

	memoryConfigPart2 = `    CHR:         start = $0000,  size = $%04X,   type = ro, file = %%O, fill = yes;
}

`

	segmentsConfigPart1 = `
SEGMENTS {
    ZEROPAGE:    load = ZP,  type = zp;
    OAM:         load = RAM, type = bss, start = $200, optional = yes;
    BSS:         load = RAM, type = bss;
    HEADER:      load = HDR, type = ro;
`

	segmentsPrgBankTemplate = `    %-12s load = %s, type = ro, start = $%04X;
`

	// Used for single-bank ROMs
	segmentsVectorsTemplate = `    VECTORS:     load = %s, type = ro, start = $%04X;
`
	segmentsConfigPart2 = `    TILES:       load = CHR, type = ro;
}
`
)

// GenerateMapperConfig generates a ca65 linker config dynamically based on the passed ROM settings.
func GenerateMapperConfig(conf Config) (string, error) {
	buf := &strings.Builder{}
	buf.WriteString(memoryConfigPart1)

	for _, bank := range conf.App.PRG {
		if _, err := fmt.Fprintf(buf, memoryPrgBankTemplate, bank.Name+":", bank.BaseAddress, len(bank.Offsets)); err != nil {
			return "", fmt.Errorf("writing memory bank line: %w", err)
		}
	}

	if _, err := fmt.Fprintf(buf, memoryConfigPart2, conf.CHRSize); err != nil {
		return "", fmt.Errorf("writing memory config: %w", err)
	}

	buf.WriteString(segmentsConfigPart1)

	for _, bank := range conf.App.PRG {
		if _, err := fmt.Fprintf(buf, segmentsPrgBankTemplate, bank.Name+":", bank.Name, bank.BaseAddress); err != nil {
			return "", fmt.Errorf("writing segment bank line: %w", err)
		}
	}

	// For single-bank ROMs, use separate VECTORS segment
	// For multi-bank ROMs, vectors are included in each bank segment
	if len(conf.App.PRG) == 1 {
		lastBank := conf.App.PRG[0]
		vectorStart := lastBank.BaseAddress + uint16(len(lastBank.Offsets)) - 6
		if _, err := fmt.Fprintf(buf, segmentsVectorsTemplate, lastBank.Name, vectorStart); err != nil {
			return "", fmt.Errorf("writing vectors segment: %w", err)
		}
	}

	if _, err := buf.WriteString(segmentsConfigPart2); err != nil {
		return "", fmt.Errorf("writing segments config: %w", err)
	}

	generated := buf.String()
	return generated, nil
}
