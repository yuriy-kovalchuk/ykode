package tui

import (
	"hash/maphash"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
)

// Code inside an answer is coloured by the language the fence names. The grammars come from chroma,
// which holds hundreds of them; the colours stay ours, because every other mark on this screen is an
// ANSI colour the terminal's own theme decides and a library's palette would not know that.
//
// A grammar is the dearest thing a frame can ask for - about a millisecond and a half for a kilobyte -
// so it is never asked the same question twice. A block that has closed is read once and kept. A block
// still arriving is read a piece at a time: the lines already written are settled in chunks, and only
// the newest chunk is read again as it grows, so the colour follows the tokens without the frame
// paying for the whole block. An untagged fence is left alone rather than guessed at, because guessing
// colours prose - a sentence about a "case" or a "type" reads as code.
const (
	// codeExactBytes is the longest closed block read in one piece. A longer one is read in chunks: a
	// string or a comment that runs across the end of a chunk loses its colour there, which is the
	// price of not stopping the screen for a generated file.
	codeExactBytes = 16 << 10
	// codeChunkBytes is how much of a block still arriving is read again per frame, at most.
	codeChunkBytes = 2 << 10
	// codeKeptBlocks is how many closed blocks one answer keeps before its cache starts over.
	codeKeptBlocks = 16
)

// codeCache is the coloured code of one answer. It belongs to one block of the transcript and goes
// when it does: a cache held for the whole session would carry the code of every answer ever drawn.
type codeCache struct {
	closed map[uint64][]string // the rows of the blocks that have closed, by what they say
	live   liveCode            // the one block still arriving, if there is one
}

// rows is a code block as rows, coloured by its language, or nil when it is not coloured: the fence
// names no language, or one we have no grammar for. The caller draws the block as it came then.
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

// liveCode is a block that is still being written, or one too long to read in one piece. It is read
// in three parts, so that a frame asks a grammar for as little as it can: the lines that will not
// change are settled a chunk at a time and never read again; the whole lines after them are read
// again only when another line is finished; and the line being written is read on its own, a few
// dozen bytes a frame. That last line is read without what came before it, so inside a comment or a
// string that runs over several lines it gets its colour when it ends, not while it is typed.
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

// chunkEnd is where a chunk of code can end: the line break before the last blank line it holds, since
// a grammar is least likely to be in the middle of something there, or else the last line break. The
// newest line is never settled: it is the one still being written.
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
	// chroma says a grammar that runs into trouble may panic rather than return, and what it is given
	// here is whatever a model wrote: a grammar that cannot cope must not take the screen with it.
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

// paintTokens walks the token stream and writes each row with the colours of its tokens, keeping the
// line breaks the source had. Every row closes what it opened, so a row drawn or cut on its own never
// leaks a colour into the next. Nothing is re-wrapped here: the rows are cut to the screen later.
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

// paint is a style as the two escapes it wraps text in. A block is thousands of tokens, and asking
// lipgloss to render each one costs more than writing the escapes it would have written.
type paint struct{ open, close string }

// codePaints reads the escapes out of the palette's styles, so the colours stay defined in one place
// and a terminal with no colour gets none.
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

// paintOf answers what one grammatical kind of text looks like here. Only the broad category is
// asked, never the exact type: chroma names hundreds of types (KeywordNamespace, LiteralStringBacktick,
// NameFunctionMagic) and each style we have covers a category of them.
func paintOf(kind chroma.TokenType) paintKind {
	switch kind.Category() {
	case chroma.Keyword:
		return paintKeyword
	case chroma.Comment:
		return paintComment
	case chroma.Name:
		// only the structure of a block reads as colour: what is defined, what is called, a tag, an
		// attribute. The identifiers in between stay the terminal's own colour, because a name coloured
		// on every mention turns the block into one solid shade, which is what the rows say elsewhere.
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

// grammars remembers which grammar a fence's word names. Asking chroma is a walk over every grammar
// it has when the word is one it does not know, and a frame asks for every block it draws.
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
