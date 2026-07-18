package structured

import (
	"regexp"
	"sort"
	"strings"
)

var (
	recordHeaderLine = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9 _()/-]{0,38}?)\s*#?\s*(\d+)\s*$`)
	recordFieldLine  = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9 /()_-]{0,30}?):\s+(\S.*?)\s*$`)
)

type recordHeader struct {
	stem   string
	number string
}

type recordCandidate struct {
	header recordHeader
	line   int
	fields map[string]string
	keys   []string
}

func detectRecordSeries(source string) []Block {
	lines := splitSourceLines(source)
	var blocks []Block

	for i := 0; i < len(lines); {
		header, ok := parseRecordHeader(lines[i].text)
		if !ok {
			i++
			continue
		}

		runStart := i
		next := nextRecordHeaderLine(lines, i+1)
		records := []recordCandidate{parseRecord(lines, i, next, header)}
		runEnd := next

		for next < len(lines) {
			nextHeader, nextOK := parseRecordHeader(lines[next].text)
			if !nextOK || !sameRecordStem(header.stem, nextHeader.stem) {
				break
			}
			afterNext := nextRecordHeaderLine(lines, next+1)
			records = append(records, parseRecord(lines, next, afterNext, nextHeader))
			runEnd = afterNext
			next = afterNext
		}

		if block, ok := renderRecordSeriesBlock(lines[runStart].offset, header.stem, records); ok {
			blocks = append(blocks, block)
		}

		if runEnd <= runStart {
			i++
			continue
		}
		i = runEnd
	}

	if len(blocks) == 0 {
		return nil
	}
	return blocks
}

func parseRecordHeader(line string) (recordHeader, bool) {
	match := recordHeaderLine.FindStringSubmatch(line)
	if match == nil {
		return recordHeader{}, false
	}
	stem := strings.TrimSpace(match[1])
	if !strings.ContainsAny(stem, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") {
		return recordHeader{}, false
	}
	return recordHeader{stem: stem, number: strings.TrimSpace(match[2])}, true
}

func sameRecordStem(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func nextRecordHeaderLine(lines []sourceLine, start int) int {
	for i := start; i < len(lines); i++ {
		if _, ok := parseRecordHeader(lines[i].text); ok {
			return i
		}
	}
	return len(lines)
}

func parseRecord(lines []sourceLine, start, end int, header recordHeader) recordCandidate {
	record := recordCandidate{
		header: header,
		line:   start,
		fields: make(map[string]string),
	}
	for i := start + 1; i < end; i++ {
		match := recordFieldLine.FindStringSubmatch(lines[i].text)
		if match == nil {
			continue
		}
		key := strings.TrimSpace(match[1])
		if _, exists := record.fields[key]; exists {
			continue
		}
		record.keys = append(record.keys, key)
		record.fields[key] = strings.TrimSpace(match[2])
	}
	return record
}

func renderRecordSeriesBlock(offset int, title string, records []recordCandidate) (Block, bool) {
	qualified := make([]recordCandidate, 0, len(records))
	for _, record := range records {
		if len(record.keys) > 0 {
			qualified = append(qualified, record)
		}
	}
	if len(qualified) < 3 {
		return Block{}, false
	}

	modalKeys, modalCount := modalRecordKeys(qualified)
	confidence := float64(modalCount) / float64(len(qualified))
	if confidence < 0.6 {
		return Block{}, false
	}

	columns := append([]string{"#"}, modalKeys...)
	rows := make([][]string, 0, len(qualified)+2)
	rows = append(rows, columns, recordSeriesSeparator(len(columns)))
	for _, record := range qualified {
		row := make([]string, 0, len(columns))
		row = append(row, record.header.number)
		for _, key := range modalKeys {
			row = append(row, record.fields[key])
		}
		rows = append(rows, row)
	}

	return Block{
		Kind:       RecordSeries,
		Title:      title,
		Markdown:   renderRecordSeriesRows(rows),
		Confidence: roundRecordConfidence(confidence),
		SrcStart:   offset,
	}, true
}

func modalRecordKeys(records []recordCandidate) ([]string, int) {
	counts := make(map[string]int)
	firstKeys := make(map[string][]string)
	for _, record := range records {
		signature := recordKeySignature(record.keys)
		counts[signature]++
		if _, exists := firstKeys[signature]; !exists {
			firstKeys[signature] = append([]string(nil), record.keys...)
		}
	}

	bestSignature := ""
	bestCount := 0
	for signature, count := range counts {
		if count > bestCount {
			bestSignature = signature
			bestCount = count
		}
	}
	return firstKeys[bestSignature], bestCount
}

func recordKeySignature(keys []string) string {
	sortedKeys := append([]string(nil), keys...)
	sort.Strings(sortedKeys)
	return strings.Join(sortedKeys, "\x00")
}

func recordSeriesSeparator(width int) []string {
	row := make([]string, width)
	for i := range row {
		row[i] = "---"
	}
	return row
}

func renderRecordSeriesRows(rows [][]string) string {
	rendered := make([]string, 0, len(rows))
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = " " + strings.ReplaceAll(cell, "|", `\|`) + " "
		}
		rendered = append(rendered, "|"+strings.Join(cells, "|")+"|")
	}
	return strings.Join(rendered, "\n")
}

func roundRecordConfidence(confidence float64) float64 {
	return float64(int(confidence*100+0.5)) / 100
}
