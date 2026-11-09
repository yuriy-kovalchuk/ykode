package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
)

// Code inside an answer is coloured by the language the fence names. The grammars come from chroma,
// which holds hundreds of them; the colours stay ours, because every other mark on this screen is an
// ANSI colour the terminal's own theme decides and a library's palette would not know that.
//
// A block longer than highlightMaxBytes is shown as it came: a lexer has no such limit, and a model
// that pastes a whole generated file would pay for it on every frame the answer is redrawn. An
// untagged fence is left alone rather than guessed at, because guessing colours prose - a sentence
// about a "case" or a "type" reads as code.
const highlightMaxBytes = 4000

// highlightedCode is a code block as rows, coloured by its language, or nil when it is not coloured:
// the fence has not closed yet, names no language, names one we have no grammar for, holds a block too
// long, or hit a grammar that gave up. The caller draws its result unstyled in every one of those cases.
func highlightedCode(code, language string, closed bool, blocks *highlightCache) []string {
	if !closed || language == "" || len(code) > highlightMaxBytes {
		return nil
	}
	if rows := blocks.rowsFor(code, language); rows != nil {
		return rows
	}
	rows := colouredCode(code, language)
	blocks.keep(code, language, rows)
	return rows
}

// colouredCode is the part worth not doing twice: it asks a grammar for the block and styles what it
// answers, which costs a hundred times the rest of drawing a code block.
func colouredCode(code, language string) (rows []string) {
	lexer := lexers.Get(language)
	if lexer == nil {
		return nil
	}
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
	return colourTokens(tokens)
}

// colourTokens walks the token stream and lays our styles over it, keeping the line breaks the source
// had. Nothing is re-wrapped here: the rows are cut to the screen later, the same way every other
// row of a code block is.
func colourTokens(tokens chroma.Iterator) []string {
	var rows []string
	line := strings.Builder{}
	for token := tokens(); token != chroma.EOF; token = tokens() {
		for i, piece := range strings.Split(token.Value, "\n") {
			if i > 0 {
				rows = append(rows, line.String())
				line.Reset()
			}
			line.WriteString(styleToken(token.Type).Render(piece))
		}
	}
	rows = append(rows, line.String())
	return rows
}

// styleToken answers what one grammatical kind of text looks like here. Only the broad category is
// asked, never the exact type: chroma names hundreds of types (KeywordNamespace, LiteralStringBacktick,
// NameFunctionMagic) and each style we have covers a category of them.
func styleToken(kind chroma.TokenType) lipgloss.Style {
	switch kind.Category() {
	case chroma.Keyword:
		return styleCodeKeyword
	case chroma.Comment:
		return styleCodeComment
	case chroma.Name:
		// only the structure of a block reads as colour: what is defined, what is called, a tag, an
		// attribute. The identifiers in between stay the terminal's own colour, because a name coloured
		// on every mention turns the block into one solid shade, which is what the rows say elsewhere.
		if kind == chroma.NameOther || kind == chroma.NameVariable {
			return styleCodeDefault
		}
		return styleCodeName
	case chroma.Literal:
		// strings and numbers: the values in the block, and the easiest thing in it to find
		return styleCodeString
	case chroma.Generic:
		// what a diff block says with its first column: the same two colours the rows of a change use
		switch kind {
		case chroma.GenericInserted:
			return styleAddMark
		case chroma.GenericDeleted:
			return styleDelMark
		}
		return styleCodeComment
	}
	return styleCodeDefault
}

// highlightCache holds the coloured rows of the code blocks one answer holds, so a frame that draws
// the answer again while it is still arriving colours only the block that changed. It belongs to one
// block of the transcript and goes when it does: a cache held for the whole session would carry the
// code of every answer ever drawn.
type highlightCache struct {
	blocks map[string][]string
}

// cacheMaxBlocks is how many blocks one answer holds before its cache starts over. A block still
// being written is a new question on every token, so the cache is thrown away rather than kept when it
// fills: the blocks of one answer are few, and the ones it left out are not asked for again.
const cacheMaxBlocks = 8

func (c *highlightCache) rowsFor(code, language string) []string {
	if c == nil {
		return nil
	}
	return c.blocks[blockKey(code, language)]
}

func (c *highlightCache) keep(code, language string, rows []string) {
	if c == nil || rows == nil {
		return
	}
	if len(c.blocks) >= cacheMaxBlocks {
		c.blocks = nil
	}
	if c.blocks == nil {
		c.blocks = map[string][]string{}
	}
	c.blocks[blockKey(code, language)] = rows
}

// blockKey is what a code block is known by: its words and the language it asked to be read as.
func blockKey(code, language string) string {
	return language + "\x00" + code
}
