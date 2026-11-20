package tui

import (
	"hash/maphash"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// chroma's grammars colour fenced code, in our colours, and never the same question twice
const (
	// codeExactBytes is the longest closed block read in one piece; longer ones go in chunks
	codeExactBytes = 16 << 10
	// codeChunkBytes is how much of a block still arriving is read again per frame, at most.
	codeChunkBytes = 2 << 10
	// codeKeptBlocks is how many closed blocks one answer keeps before its cache starts over.
	codeKeptBlocks = 16
)

// codeCache is one answer's coloured code, and goes when its block of the transcript does
type codeCache struct {
	closed map[uint64][]string // the rows of the blocks that have closed, by what they say
	live   liveCode            // the one block still arriving, if there is one
}

// rows is a block as coloured rows, or nil when the fence names no language with a grammar
func (c *codeCache) rows(code, language string, closed bool) []string {
	lexer := lexerFor(language)
	if lexer == nil {
		return nil
	}
	if c == nil {
		c = &codeCache{} // nothing to keep the rows in: they are made and handed over
	}
	if !closed {
		return c.live.draw(lexer, code)
	}
	key := blockKey(code, language)
	if rows, ok := c.closed[key]; ok {
		return rows
	}
	var rows []string
	if len(code) <= codeExactBytes {
		rows = colouredOrPlain(lexer, code)
	} else {
		rows = c.live.draw(lexer, code) // what was settled while it arrived is not read again
	}
	c.live = liveCode{}
	if len(c.closed) >= codeKeptBlocks {
		c.closed = nil
	}
	if c.closed == nil {
		c.closed = map[uint64][]string{}
	}
	c.closed[key] = rows
	return rows
}

// liveCode reads a growing block in three parts, so a frame asks a grammar for as little as can
type liveCode struct {
	seen    string   // the block as it was last drawn
	settled int      // how much of it the rows below cover
	rows    []string // the settled lines, coloured
	upto    int      // where the whole lines read last time end; never before settled
	whole   []string // those lines, coloured
}

// draw colours the block as it stands now, reading only what was written since the last look.
func (l *liveCode) draw(lexer chroma.Lexer, code string) []string {
	if len(code) < l.upto || code[:l.upto] != l.seen[:l.upto] {
		*l = liveCode{} // another block, or one whose beginning changed
	}
	l.seen = code
	for len(code)-l.settled > codeChunkBytes {
		cut := chunkEnd(code[l.settled:])
		if cut < 0 {
			break // one line longer than a chunk: it is read whole, there is no line to settle at
		}
		l.rows = append(l.rows, colouredOrPlain(lexer, code[l.settled:l.settled+cut])...)
		l.settled += cut + 1 // the line break belongs to neither side
		l.upto, l.whole = l.settled, nil
	}
	rows := l.rows[:len(l.rows):len(l.rows)]
	writing := l.settled // where the line being written begins
	if end := strings.LastIndexByte(code, '\n'); end >= l.settled {
		if end != l.upto || l.whole == nil {
			l.upto, l.whole = end, colouredOrPlain(lexer, code[l.settled:end])
		}
		rows, writing = append(rows, l.whole...), end+1
	}
	return append(rows, colouredOrPlain(lexer, code[writing:])...)
}

// chunkEnd is where a chunk may end: the break before the last blank line, never the newest line
func chunkEnd(code string) int {
	reach := code[:min(len(code), codeChunkBytes)]
	if at := strings.LastIndex(reach, "\n\n"); at > 0 {
		return at
	}
	return strings.LastIndex(reach, "\n")
}

// colouredOrPlain is a piece of code as rows: coloured, or as it was written when the grammar gave up.
func colouredOrPlain(lexer chroma.Lexer, code string) []string {
	if rows := coloured(lexer, code); rows != nil {
		return rows
	}
	return strings.Split(code, "\n")
}

// coloured asks a grammar for the code and lays our styles over what it answers.
func coloured(lexer chroma.Lexer, code string) (rows []string) {
	// a grammar may panic on what a model wrote, so it is caught rather than taking the screen down
	defer func() {
		if recover() != nil {
			rows = nil
		}
	}()
	tokens, err := lexer.Tokenise(nil, code)
	if err != nil {
		return nil
	}
	return paintTokens(tokens, codePaints())
}

// paintTokens writes each row with its tokens' colours, closing every row it opens
func paintTokens(tokens chroma.Iterator, paints [paintCount]paint) []string {
	var rows []string
	var line strings.Builder
	open := paintDefault // what the row is writing in now
	closeRow := func() {
		line.WriteString(paints[open].close)
		rows = append(rows, line.String())
		line.Reset()
		open = paintDefault
	}
	for token := tokens(); token != chroma.EOF; token = tokens() {
		kind := paintOf(token.Type)
		for i, piece := range strings.Split(token.Value, "\n") {
			if i > 0 {
				closeRow()
			}
			if piece == "" {
				continue
			}
			if kind != open {
				line.WriteString(paints[open].close)
				line.WriteString(paints[kind].open)
				open = kind
			}
			line.WriteString(piece)
		}
	}
	closeRow()
	return rows
}

// paintKind is one of the few ways a piece of code is drawn.
type paintKind int

const (
	paintDefault paintKind = iota
	paintKeyword
	paintComment
	paintName
	paintString
	paintAdded
	paintRemoved
	paintCount
)

// paint is a style as two escapes: a block is thousands of tokens, and lipgloss costs more
type paint struct{ open, close string }

// codePaints reads the escapes out of the palette's styles, so colours are defined in one place
func codePaints() [paintCount]paint {
	var paints [paintCount]paint
	for kind, style := range [paintCount]lipgloss.Style{
		paintDefault: styleCodeDefault,
		paintKeyword: styleCodeKeyword,
		paintComment: styleCodeComment,
		paintName:    styleCodeName,
		paintString:  styleCodeString,
		paintAdded:   styleAddMark,
		paintRemoved: styleDelMark,
	} {
		open, close, _ := strings.Cut(style.Render("\x00"), "\x00")
		paints[kind] = paint{open: open, close: close}
	}
	return paints
}

// paintOf answers by broad grammatical category; chroma names hundreds of exact types
func paintOf(kind chroma.TokenType) paintKind {
	switch kind.Category() {
	case chroma.Keyword:
		return paintKeyword
	case chroma.Comment:
		return paintComment
	case chroma.Name:
		// only structure reads as colour: the identifiers between keep the terminal's own colour
		if kind == chroma.NameOther || kind == chroma.NameVariable {
			return paintDefault
		}
		return paintName
	case chroma.Literal:
		// strings and numbers: the values in the block, and the easiest thing in it to find
		return paintString
	case chroma.Generic:
		// what a diff block says with its first column: the same two colours the rows of a change use
		switch kind {
		case chroma.GenericInserted:
			return paintAdded
		case chroma.GenericDeleted:
			return paintRemoved
		}
		return paintComment
	}
	return paintDefault
}

// grammars remembers which grammar a fence's word names, since asking walks every grammar
var grammars sync.Map // language -> chroma.Lexer, or nil for a word no grammar answers to

func lexerFor(language string) chroma.Lexer {
	if language == "" {
		return nil
	}
	if known, ok := grammars.Load(language); ok {
		lexer, _ := known.(chroma.Lexer)
		return lexer
	}
	lexer := lexers.Get(language)
	if lexer != nil {
		lexer = chroma.Coalesce(lexer) // one token per run of a kind, not one per character
	}
	grammars.Store(language, lexer)
	return lexer
}

var blockSeed = maphash.MakeSeed()

// blockKey is what a closed block is known by: its words and the language it asked to be read as.
func blockKey(code, language string) uint64 {
	var h maphash.Hash
	h.SetSeed(blockSeed)
	h.WriteString(language)
	h.WriteByte(0)
	h.WriteString(code)
	return h.Sum64()
}
