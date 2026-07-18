package structured

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

func detectHTMLTables(source string) []Block {
	if !strings.Contains(source, "<table") {
		return nil
	}

	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return nil
	}

	var blocks []Block
	searchOffset := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "table" {
			rows := collectHTMLTableRows(n)
			if markdown := renderHTMLTable(rows); markdown != "" {
				start := nextHTMLTableOffset(source, searchOffset)
				if start >= 0 {
					searchOffset = start + len("<table")
				}
				blocks = append(blocks, Block{
					Kind:       HTMLTable,
					Title:      fmt.Sprintf("Table %d", len(blocks)+1),
					Markdown:   markdown,
					Confidence: 1.0,
					SrcStart:   start,
				})
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	if len(blocks) == 0 {
		return nil
	}
	return blocks
}

type htmlTableRow struct {
	cells []string
}

func collectHTMLTableRows(table *html.Node) []htmlTableRow {
	var rows []htmlTableRow
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			if row := collectHTMLTableRow(n); len(row.cells) > 0 {
				rows = append(rows, row)
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(table)
	return rows
}

func collectHTMLTableRow(row *html.Node) htmlTableRow {
	var out htmlTableRow
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || (child.Data != "th" && child.Data != "td") {
			continue
		}
		out.cells = append(out.cells, htmlCellText(child))
	}
	return out
}

func htmlCellText(cell *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			parts = append(parts, n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(cell)

	text := strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
	return strings.ReplaceAll(text, "|", `\|`)
}

func renderHTMLTable(rows []htmlTableRow) string {
	if len(rows) == 0 || len(rows[0].cells) == 0 {
		return ""
	}

	header := rows[0].cells
	width := len(header)
	rendered := []string{
		renderHTMLTableRow(header, width),
		renderHTMLTableSeparator(width),
	}
	for _, row := range rows[1:] {
		rendered = append(rendered, renderHTMLTableRow(row.cells, width))
	}
	return strings.Join(rendered, "\n")
}

func renderHTMLTableRow(cells []string, width int) string {
	padded := make([]string, width)
	copy(padded, cells)
	for i := range padded {
		padded[i] = " " + padded[i] + " "
	}
	return "|" + strings.Join(padded, "|") + "|"
}

func renderHTMLTableSeparator(width int) string {
	cells := make([]string, width)
	for i := range cells {
		cells[i] = " --- "
	}
	return "|" + strings.Join(cells, "|") + "|"
}

func nextHTMLTableOffset(source string, from int) int {
	index := strings.Index(source[from:], "<table")
	if index < 0 {
		return -1
	}
	return from + index
}
