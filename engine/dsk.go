package engine

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

// Apple II .DSK format constants.
// From reference/startup.py AppleDisk class and docs/disk-format.md.
const (
	dskSize        = 143360 // 35 tracks × 16 sectors × 256 bytes
	sectorsPerTrack = 16
	bytesPerSector  = 256
	blockSize       = 512   // UCSD Pascal block = 2 sectors
	blocksPerTrack  = 8
)

// Sector interleave table: UCSD Pascal logical sector → DOS 3.3 physical sector.
// From reference/startup.py: DskTable[] and docs/disk-format.md.
var sectorInterleave = [16]int{0, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 15}

// SCENARIO.DATA zone indices (from reference/startup.py ZScn class).
const zoneChar = 5 // ZCHAR — character records

// readBlock reads a 512-byte UCSD Pascal block from a .DSK image.
func readBlock(dsk []byte, blockNum int) []byte {
	track := blockNum / blocksPerTrack
	firstSector := (blockNum & 7) * 2
	buf := make([]byte, blockSize)
	for i := 0; i < 2; i++ {
		logicalSector := firstSector + i
		physicalSector := sectorInterleave[logicalSector]
		offset := (track*sectorsPerTrack + physicalSector) * bytesPerSector
		copy(buf[i*bytesPerSector:], dsk[offset:offset+bytesPerSector])
	}
	return buf
}

// readBlocks reads multiple consecutive blocks.
func readBlocks(dsk []byte, startBlock, count int) []byte {
	result := make([]byte, 0, count*blockSize)
	for blk := startBlock; blk < startBlock+count; blk++ {
		result = append(result, readBlock(dsk, blk)...)
	}
	return result
}

// dskDirEntry represents a file in the UCSD Pascal volume directory.
type dskDirEntry struct {
	Name      string
	FirstBlk  int
	LastBlk   int
	Kind      int
	BlockCount int
}

// listDirectory parses the UCSD Pascal volume directory (blocks 2-5).
// From reference/startup.py AppleDisk.list_directory().
func listDirectory(dsk []byte) []dskDirEntry {
	dirData := readBlocks(dsk, 2, 4) // blocks 2-5
	var files []dskDirEntry
	for entry := 1; entry < 78; entry++ { // skip entry 0 (volume header)
		pos := entry * 26
		if pos+26 > len(dirData) {
			break
		}
		firstBlk := int(binary.LittleEndian.Uint16(dirData[pos:]))
		lastBlk := int(binary.LittleEndian.Uint16(dirData[pos+2:]))
		fkind := int(binary.LittleEndian.Uint16(dirData[pos+4:])) & 0xF
		nameLen := int(dirData[pos+6])
		if nameLen > 15 {
			nameLen = 15
		}
		name := string(dirData[pos+7 : pos+7+nameLen])

		if firstBlk > 0 && lastBlk > firstBlk && lastBlk <= 280 {
			files = append(files, dskDirEntry{
				Name:      name,
				FirstBlk:  firstBlk,
				LastBlk:   lastBlk,
				Kind:      fkind,
				BlockCount: lastBlk - firstBlk,
			})
		}
	}
	return files
}

// findFile finds a file by name on the disk. Returns first block and block count, or error.
func findFile(dsk []byte, name string) (int, int, error) {
	for _, entry := range listDirectory(dsk) {
		if strings.EqualFold(entry.Name, name) {
			return entry.FirstBlk, entry.BlockCount, nil
		}
	}
	return 0, 0, fmt.Errorf("file %q not found on disk", name)
}

// scenarioTOC holds the parsed SCENARIO.DATA table of contents.
// From reference/startup.py ScenarioTOC.
type scenarioTOC struct {
	GameName string
	RecPer2B [8]int // records per 2-block cache chunk, per zone
	RecPerDk [8]int // records per disk, per zone
	BlOff    [8]int // block offset per zone
}

// parseTOC reads the SCENARIO.DATA TOC from the first 2 blocks.
// Layout from reference/startup.py ScenarioTOC.from_bytes():
//
//	0x00: GAMENAME (STRING[40] = 42 bytes with padding)
//	0x2A: RECPER2B (8 × uint16)
//	0x3A: RECPERDK (8 × uint16)
//	0x4A: UNUSEDXX (8 × uint16)
//	0x5A: BLOFF (8 × uint16)
func parseTOC(data []byte) scenarioTOC {
	var toc scenarioTOC

	// GAMENAME: Pascal string, length byte + up to 40 chars
	nameLen := int(data[0])
	if nameLen > 40 {
		nameLen = 40
	}
	toc.GameName = string(data[1 : 1+nameLen])

	// Arrays start at offset 0x2A (42)
	for i := 0; i < 8; i++ {
		toc.RecPer2B[i] = int(binary.LittleEndian.Uint16(data[0x2A+i*2:]))
		toc.RecPerDk[i] = int(binary.LittleEndian.Uint16(data[0x3A+i*2:]))
		// skip UNUSEDXX at 0x4A
		toc.BlOff[i] = int(binary.LittleEndian.Uint16(data[0x5A+i*2:]))
	}

	return toc
}

// readPascalString reads a length-prefixed Pascal string.
func readPascalString(data []byte, offset, maxLen int) string {
	length := int(data[offset])
	if length > maxLen {
		length = maxLen
	}
	return string(data[offset+1 : offset+1+length])
}

// unpack5BitArray unpacks UCSD Pascal PACKED ARRAY of 5-bit values.
// 3 elements per 16-bit word, packed from LSB.
// From reference/startup.py TChar._unpack_5bit_array().
func unpack5BitArray(data []byte, offset, count int) []int {
	values := make([]int, 0, count)
	wordIdx := 0
	elemInWord := 0
	var word uint16
	for i := 0; i < count; i++ {
		if elemInWord == 0 {
			word = binary.LittleEndian.Uint16(data[offset+wordIdx*2:])
		}
		values = append(values, int((word>>(uint(elemInWord)*5))&0x1F))
		elemInWord++
		if elemInWord >= 3 {
			elemInWord = 0
			wordIdx++
		}
	}
	return values
}

// parseTCHAR reads a 208-byte TCHAR record and converts to our Character struct.
// Field mapping from docs/data-structures.md and reference/startup.py TChar.from_bytes().
func parseTCHAR(data []byte, offset int) *Character {
	d := data[offset:]

	name := readPascalString(d, 0x00, 15)
	status := int(binary.LittleEndian.Uint16(d[0x28:]))

	// Skip LOST characters (empty slots)
	if status == 7 { // LOST
		return nil
	}

	race := int(binary.LittleEndian.Uint16(d[0x22:]))
	class := int(binary.LittleEndian.Uint16(d[0x24:]))
	age := int(binary.LittleEndian.Uint16(d[0x26:]))
	align := int(binary.LittleEndian.Uint16(d[0x2A:]))

	// Attributes: packed 5-bit array at 0x2C (6 values: STR, IQ, PIE, VIT, AGI, LCK)
	attribs := unpack5BitArray(d, 0x2C, 6)

	// Gold: TWIZLONG at 0x34 — base-10000 arithmetic (p-code proc 35, LDCI 10000)
	// Each word holds 0-9999. Value = low + mid*10000 + high*100000000
	goldLow := int(binary.LittleEndian.Uint16(d[0x34:]))
	goldMid := int(binary.LittleEndian.Uint16(d[0x36:]))
	goldHigh := int(binary.LittleEndian.Uint16(d[0x38:]))
	gold := goldLow + goldMid*10000 + goldHigh*100000000

	// XP: TWIZLONG at 0x7C — same base-10000 format
	xpLow := int(binary.LittleEndian.Uint16(d[0x7C:]))
	xpMid := int(binary.LittleEndian.Uint16(d[0x7E:]))
	xpHigh := int(binary.LittleEndian.Uint16(d[0x80:]))
	xp := xpLow + xpMid*10000 + xpHigh*100000000

	level := int(binary.LittleEndian.Uint16(d[0x84:]))
	hpLeft := int(binary.LittleEndian.Uint16(d[0x86:]))
	hpMax := int(binary.LittleEndian.Uint16(d[0x88:]))
	ac := int(binary.LittleEndian.Uint16(d[0xB0:]))

	// Mage spells: 7 × uint16 at 0x92
	var mageSpells [7]int
	for i := 0; i < 7; i++ {
		mageSpells[i] = int(binary.LittleEndian.Uint16(d[0x92+i*2:]))
	}

	// Priest spells: 7 × uint16 at 0xA0
	var priestSpells [7]int
	for i := 0; i < 7; i++ {
		priestSpells[i] = int(binary.LittleEndian.Uint16(d[0xA0+i*2:]))
	}

	// Items: 8 possessions at 0x3C, each 8 bytes (4 words)
	// {EQUIPED(2), CURSED(2), IDENTIF(2), EQINDEX(2)} — matches p-code item record
	possCnt := int(binary.LittleEndian.Uint16(d[0x3A:]))
	var items [8]Possession
	for i := 0; i < 8 && i < possCnt; i++ {
		base := 0x3C + i*8
		items[i] = Possession{
			Equipped:   binary.LittleEndian.Uint16(d[base:]) != 0,
			Cursed:     binary.LittleEndian.Uint16(d[base+2:]) != 0,
			Identified: binary.LittleEndian.Uint16(d[base+4:]) != 0,
			ItemIndex:  int(binary.LittleEndian.Uint16(d[base+6:])),
		}
	}
	itemCount := possCnt
	if itemCount > 8 {
		itemCount = 8
	}

	// Map align: original uses 0=UNALIGN, 1=GOOD, 2=NEUTRAL, 3=EVIL
	// Our code uses 0=Good, 1=Neutral, 2=Evil
	var ourAlign Alignment
	switch align {
	case 1:
		ourAlign = Good
	case 2:
		ourAlign = Neutral
	case 3:
		ourAlign = Evil
	default:
		ourAlign = Neutral
	}

	// Map status: original 0=OK,1=AFRAID,2=ASLEEP,3=PLYZE,4=STONED,5=DEAD,6=ASHES,7=LOST
	// Our code: 0=OK,1=Asleep,2=Afraid,3=Paralyzed,4=Stoned,5=Dead,6=Ashed,7=Lost
	statusMap := map[int]Status{
		0: OK, 1: Afraid, 2: Asleep, 3: Paralyzed,
		4: Stoned, 5: Dead, 6: Ashed, 7: Lost,
	}
	ourStatus := statusMap[status]

	// Map race: original 0=NORACE,1=HUMAN,2=ELF,3=DWARF,4=GNOME,5=HOBBIT
	// Our code: 0=Human,1=Elf,2=Dwarf,3=Gnome,4=Hobbit
	ourRace := Human
	if race >= 1 && race <= 5 {
		ourRace = Race(race - 1)
	}

	c := &Character{
		Name:         strings.TrimSpace(name),
		Race:         ourRace,
		Class:        Class(class),
		Alignment:    ourAlign,
		Status:       ourStatus,
		Level:        level,
		XP:           xp,
		HP:           hpLeft,
		MaxHP:        hpMax,
		Gold:         gold,
		Age:          age,
		Strength:     attribs[0],
		IQ:           attribs[1],
		Piety:        attribs[2],
		Vitality:     attribs[3],
		Agility:      attribs[4],
		Luck:         attribs[5],
		AC:           ac,
		MageSpells:      mageSpells,
		PriestSpells:    priestSpells,
		MaxMageSpells:   mageSpells,
		MaxPriestSpells: priestSpells,
		Items:        items,
		ItemCount:    itemCount,
	}
	return c
}

// ImportFromDSK reads all characters from a Wizardry .DSK scenario disk image.
// Returns the game name and a slice of characters (skipping LOST/empty slots).
//
// Process:
//  1. Read .DSK file (143360 bytes)
//  2. Find SCENARIO.DATA in UCSD Pascal directory
//  3. Parse TOC to get ZCHAR zone offset and record count
//  4. Read each TCHAR record (208 bytes) and convert to Character
func ImportFromDSK(path string) (string, []*Character, error) {
	path = expandPath(path)
	dsk, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("read disk: %w", err)
	}
	if len(dsk) != dskSize {
		return "", nil, fmt.Errorf("not a standard Apple II .DSK image: expected %d bytes, got %d", dskSize, len(dsk))
	}

	// Find SCENARIO.DATA on disk
	firstBlk, blockCount, err := findFile(dsk, "SCENARIO.DATA")
	if err != nil {
		return "", nil, fmt.Errorf("SCENARIO.DATA not found on disk image")
	}

	// Read the full SCENARIO.DATA
	scenData := readBlocks(dsk, firstBlk, blockCount)

	// Parse TOC from first 2 blocks (1024 bytes)
	toc := parseTOC(scenData)

	// Read character records from ZCHAR zone
	charCount := toc.RecPerDk[zoneChar]
	if charCount <= 0 || charCount > 100 {
		return toc.GameName, nil, fmt.Errorf("invalid character count: %d", charCount)
	}

	// GETREC: block = BLOFF[zone] + 2 * (index / RECPER2B[zone])
	//         offset = record_size * (index % RECPER2B[zone])
	// BLOFF is relative to SCENARIO.DATA start, so we read from scenData.
	recPer2B := toc.RecPer2B[zoneChar]
	bloff := toc.BlOff[zoneChar]
	if recPer2B <= 0 {
		return toc.GameName, nil, fmt.Errorf("invalid recper2b for ZCHAR: %d", recPer2B)
	}

	var chars []*Character
	for i := 0; i < charCount; i++ {
		// Calculate block and offset within that block's data
		blockIdx := bloff + 2*(i/recPer2B)
		bufOffset := 208 * (i % recPer2B)

		// Read the 2-block (1024-byte) chunk
		chunkStart := blockIdx * blockSize
		if chunkStart+1024 > len(scenData) {
			break
		}
		chunk := scenData[chunkStart : chunkStart+1024]

		if bufOffset+208 > len(chunk) {
			break
		}

		c := parseTCHAR(chunk, bufOffset)
		if c != nil {
			chars = append(chars, c)
		}
	}

	return toc.GameName, chars, nil
}
