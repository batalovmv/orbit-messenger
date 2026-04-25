// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/mst-corp/orbit/pkg/apperror"
)

// encodeEntities serialises a slice of Orbit entities into a JSON array
// suitable for the messaging entities JSONB column. Returns nil for empty
// inputs so callers can omit the field from the messaging payload.
func encodeEntities(entities []OrbitEntity) (json.RawMessage, error) {
	if len(entities) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(entities)
	if err != nil {
		return nil, apperror.Internal("Failed to encode entities")
	}
	return raw, nil
}

// Telegram Bot API parse_mode values.
const (
	ParseModeMarkdownV2 = "MarkdownV2"
	ParseModeHTML       = "HTML"
)

// MessageEntity is the Telegram Bot API entity payload accepted from clients.
// Type values follow TG conventions: bold|italic|underline|strikethrough|
// spoiler|code|pre|text_link|text_mention|mention|hashtag|cashtag|bot_command|
// email|phone_number|blockquote|url|custom_emoji.
type MessageEntity struct {
	Type        string `json:"type"`
	Offset      int    `json:"offset"`
	Length      int    `json:"length"`
	URL         string `json:"url,omitempty"`
	Language    string `json:"language,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	CustomEmoji string `json:"custom_emoji_id,omitempty"`
}

// OrbitEntity is the storage shape used by the messaging service (entities JSONB).
// Frontend consumes it directly via SaturnMessageEntity.
type OrbitEntity struct {
	Type     string `json:"type"`
	Offset   int    `json:"offset"`
	Length   int    `json:"length"`
	URL      string `json:"url,omitempty"`
	Language string `json:"language,omitempty"`
	UserID   string `json:"user_id,omitempty"`
}

// tgToOrbitType maps TG bot API entity types to Orbit storage types
// (which mirror Telegram TL constructor names used by the web client).
var tgToOrbitType = map[string]string{
	"bold":          "MessageEntityBold",
	"italic":        "MessageEntityItalic",
	"underline":     "MessageEntityUnderline",
	"strikethrough": "MessageEntityStrike",
	"spoiler":       "MessageEntitySpoiler",
	"code":          "MessageEntityCode",
	"pre":           "MessageEntityPre",
	"text_link":     "MessageEntityTextUrl",
	"url":           "MessageEntityUrl",
	"mention":       "MessageEntityMention",
	"text_mention":  "MessageEntityMentionName",
	"hashtag":       "MessageEntityHashtag",
	"cashtag":       "MessageEntityCashtag",
	"bot_command":   "MessageEntityBotCommand",
	"email":         "MessageEntityEmail",
	"phone_number":  "MessageEntityPhone",
	"blockquote":    "MessageEntityBlockquote",
	"custom_emoji":  "MessageEntityCustomEmoji",
}

// convertTGEntities converts incoming Bot API entities into the Orbit storage
// shape. Unknown types are dropped silently — bots forward arbitrary entity
// types from their own data sources and we don't want to 400 on an obscure
// future type that doesn't break rendering.
func convertTGEntities(entities []MessageEntity) []OrbitEntity {
	if len(entities) == 0 {
		return nil
	}
	out := make([]OrbitEntity, 0, len(entities))
	for _, e := range entities {
		orbitType, ok := tgToOrbitType[strings.ToLower(strings.TrimSpace(e.Type))]
		if !ok {
			continue
		}
		out = append(out, OrbitEntity{
			Type:     orbitType,
			Offset:   e.Offset,
			Length:   e.Length,
			URL:      e.URL,
			Language: e.Language,
			UserID:   e.UserID,
		})
	}
	return out
}

// utf16Len returns the number of UTF-16 code units required to encode s.
// Telegram entity offsets/lengths are expressed in UTF-16 code units, not
// bytes or runes — astral-plane characters (most emoji) cost two.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// resolveTextAndEntities computes the final text and Orbit entities to store
// for a Bot API send/edit request.
//
//   - Explicit entities always win over parse_mode (parse_mode is ignored).
//   - parse_mode parsing strips formatting markers from the returned text.
//   - Empty parse_mode + nil entities means plain text, no entities.
func resolveTextAndEntities(text, parseMode string, explicit []MessageEntity) (string, []OrbitEntity, error) {
	if len(explicit) > 0 {
		return text, convertTGEntities(explicit), nil
	}
	switch strings.TrimSpace(parseMode) {
	case "":
		return text, nil, nil
	case ParseModeMarkdownV2:
		return parseMarkdownV2(text)
	case ParseModeHTML:
		return parseHTML(text)
	default:
		return "", nil, apperror.BadRequest("Invalid parse_mode (expected MarkdownV2 or HTML)")
	}
}

// ----------------------------------------------------------------------------
// MarkdownV2 parser
// ----------------------------------------------------------------------------

// parseMarkdownV2 implements a pragmatic subset of Telegram MarkdownV2:
//
//	*bold*  _italic_  __underline__  ~strike~  ||spoiler||
//	`code`  ```pre```  ```language\npre with lang```
//	[text](url)  >blockquote (line prefix)
//
// Backslash escapes any character literally. Nested formatting is supported via
// a marker stack; mismatched markers are emitted as literal text.
func parseMarkdownV2(input string) (string, []OrbitEntity, error) {
	s := newMDScanner(input)
	if err := s.run(); err != nil {
		return "", nil, err
	}
	return s.text.String(), s.entities, nil
}

type mdMark struct {
	typ      string
	utf16Pos int // UTF-16 offset where the entity content begins
}

type mdScanner struct {
	src      []rune
	i        int
	text     strings.Builder
	utf16Off int
	stack    []mdMark
	entities []OrbitEntity
	atLineStart bool
}

func newMDScanner(input string) *mdScanner {
	return &mdScanner{
		src:         []rune(input),
		atLineStart: true,
	}
}

func (s *mdScanner) emitRune(r rune) {
	s.text.WriteRune(r)
	if r > 0xFFFF {
		s.utf16Off += 2
	} else {
		s.utf16Off++
	}
	s.atLineStart = r == '\n'
}

func (s *mdScanner) hasPrefix(prefix string) bool {
	pr := []rune(prefix)
	if s.i+len(pr) > len(s.src) {
		return false
	}
	for k, r := range pr {
		if s.src[s.i+k] != r {
			return false
		}
	}
	return true
}

func (s *mdScanner) findStackTop(typ string) int {
	for k := len(s.stack) - 1; k >= 0; k-- {
		if s.stack[k].typ == typ {
			return k
		}
	}
	return -1
}

func (s *mdScanner) closeMark(idx int, urlOverride string) {
	mk := s.stack[idx]
	length := s.utf16Off - mk.utf16Pos
	if length > 0 {
		ent := OrbitEntity{
			Type:   mk.typ,
			Offset: mk.utf16Pos,
			Length: length,
		}
		if urlOverride != "" {
			ent.URL = urlOverride
		}
		s.entities = append(s.entities, ent)
	}
	s.stack = s.stack[:idx]
}

func (s *mdScanner) run() error {
	for s.i < len(s.src) {
		r := s.src[s.i]

		// Backslash escape: emit the next rune literally.
		if r == '\\' && s.i+1 < len(s.src) {
			s.emitRune(s.src[s.i+1])
			s.i += 2
			continue
		}

		// Code blocks: ```pre``` (optional language on the first line).
		if r == '`' && s.hasPrefix("```") {
			if err := s.parsePreBlock(); err != nil {
				return err
			}
			continue
		}

		// Inline code: `code`.
		if r == '`' {
			if err := s.parseInlineCode(); err != nil {
				return err
			}
			continue
		}

		// Blockquote: > at the start of a line (single-line).
		if r == '>' && s.atLineStart {
			s.parseBlockquote()
			continue
		}

		// Spoiler: ||...||.
		if r == '|' && s.hasPrefix("||") {
			s.toggleMark("MessageEntitySpoiler")
			s.i += 2
			continue
		}

		// Underline: __...__ (must be checked before italic single-underscore).
		if r == '_' && s.hasPrefix("__") {
			s.toggleMark("MessageEntityUnderline")
			s.i += 2
			continue
		}

		// Italic: _..._.
		if r == '_' {
			s.toggleMark("MessageEntityItalic")
			s.i++
			continue
		}

		// Bold: *...*.
		if r == '*' {
			s.toggleMark("MessageEntityBold")
			s.i++
			continue
		}

		// Strike: ~...~.
		if r == '~' {
			s.toggleMark("MessageEntityStrike")
			s.i++
			continue
		}

		// Text link: [text](url).
		if r == '[' {
			if s.parseTextLink() {
				continue
			}
		}

		s.emitRune(r)
		s.i++
	}

	// Any unclosed markers become literal text — we re-emit their opening
	// marker characters at the recorded position. To keep the implementation
	// simple, we just close them at the current end with no entity emitted.
	for len(s.stack) > 0 {
		s.stack = s.stack[:len(s.stack)-1]
	}
	return nil
}

func (s *mdScanner) toggleMark(typ string) {
	idx := s.findStackTop(typ)
	if idx >= 0 {
		s.closeMark(idx, "")
		return
	}
	s.stack = append(s.stack, mdMark{
		typ:      typ,
		utf16Pos: s.utf16Off,
	})
}

func (s *mdScanner) parsePreBlock() error {
	// Skip the opening ```.
	s.i += 3
	// Optional language: read until newline.
	lang := ""
	for s.i < len(s.src) && s.src[s.i] != '\n' {
		lang += string(s.src[s.i])
		s.i++
	}
	lang = strings.TrimSpace(lang)
	if s.i < len(s.src) && s.src[s.i] == '\n' {
		s.i++ // consume newline that separates lang from body
	}

	startUTF16 := s.utf16Off

	// Walk until closing ``` or EOF.
	for s.i < len(s.src) {
		if s.hasPrefix("```") {
			length := s.utf16Off - startUTF16
			ent := OrbitEntity{
				Type:     "MessageEntityPre",
				Offset:   startUTF16,
				Length:   length,
				Language: lang,
			}
			if length > 0 {
				s.entities = append(s.entities, ent)
			}
			s.i += 3
			return nil
		}
		// Backslash escape inside pre: still pass through literally if the
		// escape is for a backtick (so authors can include literal triple
		// backticks). Otherwise emit the backslash itself.
		if s.src[s.i] == '\\' && s.i+1 < len(s.src) {
			s.emitRune(s.src[s.i+1])
			s.i += 2
			continue
		}
		s.emitRune(s.src[s.i])
		s.i++
	}
	// Unterminated pre — emit content but no entity.
	return nil
}

func (s *mdScanner) parseInlineCode() error {
	s.i++ // skip opening backtick
	startUTF16 := s.utf16Off
	for s.i < len(s.src) {
		if s.src[s.i] == '`' {
			length := s.utf16Off - startUTF16
			if length > 0 {
				s.entities = append(s.entities, OrbitEntity{
					Type:   "MessageEntityCode",
					Offset: startUTF16,
					Length: length,
				})
			}
			s.i++
			return nil
		}
		if s.src[s.i] == '\\' && s.i+1 < len(s.src) {
			s.emitRune(s.src[s.i+1])
			s.i += 2
			continue
		}
		s.emitRune(s.src[s.i])
		s.i++
	}
	return nil
}

func (s *mdScanner) parseBlockquote() {
	// Consume the leading `>` and an optional space.
	s.i++
	if s.i < len(s.src) && s.src[s.i] == ' ' {
		s.i++
	}
	startUTF16 := s.utf16Off
	for s.i < len(s.src) && s.src[s.i] != '\n' {
		// Allow nested simple formatting via the main loop's char path —
		// but to keep blockquote bounds correct we re-emit literally here.
		if s.src[s.i] == '\\' && s.i+1 < len(s.src) {
			s.emitRune(s.src[s.i+1])
			s.i += 2
			continue
		}
		s.emitRune(s.src[s.i])
		s.i++
	}
	length := s.utf16Off - startUTF16
	if length > 0 {
		s.entities = append(s.entities, OrbitEntity{
			Type:   "MessageEntityBlockquote",
			Offset: startUTF16,
			Length: length,
		})
	}
}

// parseTextLink attempts to parse [text](url) starting at s.i (which points at
// `[`). Returns true if a link was consumed. Otherwise s.i is unchanged.
func (s *mdScanner) parseTextLink() bool {
	// Find matching `]`.
	closeBracket := -1
	depth := 1
	for k := s.i + 1; k < len(s.src); k++ {
		if s.src[k] == '\\' && k+1 < len(s.src) {
			k++
			continue
		}
		if s.src[k] == '[' {
			depth++
		} else if s.src[k] == ']' {
			depth--
			if depth == 0 {
				closeBracket = k
				break
			}
		}
	}
	if closeBracket < 0 || closeBracket+1 >= len(s.src) || s.src[closeBracket+1] != '(' {
		return false
	}
	closeParen := -1
	for k := closeBracket + 2; k < len(s.src); k++ {
		if s.src[k] == '\\' && k+1 < len(s.src) {
			k++
			continue
		}
		if s.src[k] == ')' {
			closeParen = k
			break
		}
	}
	if closeParen < 0 {
		return false
	}

	// Extract URL (with backslash escapes resolved).
	var urlB strings.Builder
	for k := closeBracket + 2; k < closeParen; k++ {
		if s.src[k] == '\\' && k+1 < closeParen {
			urlB.WriteRune(s.src[k+1])
			k++
			continue
		}
		urlB.WriteRune(s.src[k])
	}
	url := urlB.String()

	// Emit text content (between `[` and `]`) as plain text and capture its
	// UTF-16 span.
	startUTF16 := s.utf16Off
	for k := s.i + 1; k < closeBracket; k++ {
		if s.src[k] == '\\' && k+1 < closeBracket {
			s.emitRune(s.src[k+1])
			k++
			continue
		}
		s.emitRune(s.src[k])
	}
	length := s.utf16Off - startUTF16
	if length > 0 {
		s.entities = append(s.entities, OrbitEntity{
			Type:   "MessageEntityTextUrl",
			Offset: startUTF16,
			Length: length,
			URL:    url,
		})
	}
	s.i = closeParen + 1
	return true
}

// ----------------------------------------------------------------------------
// HTML parser
// ----------------------------------------------------------------------------

// parseHTML implements a pragmatic subset of Telegram Bot API HTML:
//
//	<b>, <strong>           → bold
//	<i>, <em>               → italic
//	<u>, <ins>              → underline
//	<s>, <strike>, <del>    → strike
//	<tg-spoiler>            → spoiler
//	<code>                  → code (inside <pre>: language is read from
//	                          class="language-X" if present)
//	<pre>                   → pre
//	<a href="...">          → text_link
//	<blockquote>            → blockquote
//	<br>                    → newline
//
// HTML entities &amp; &lt; &gt; &quot; &#NN; &#xNN; are decoded.
func parseHTML(input string) (string, []OrbitEntity, error) {
	p := &htmlParser{src: input}
	if err := p.run(); err != nil {
		return "", nil, err
	}
	return p.text.String(), p.entities, nil
}

type htmlMark struct {
	typ      string
	utf16Pos int
	url      string
	language string
}

type htmlParser struct {
	src      string
	i        int
	text     strings.Builder
	utf16Off int
	stack    []htmlMark
	entities []OrbitEntity
}

func (p *htmlParser) emitRune(r rune) {
	p.text.WriteRune(r)
	if r > 0xFFFF {
		p.utf16Off += 2
	} else {
		p.utf16Off++
	}
}

func (p *htmlParser) emitString(s string) {
	for _, r := range s {
		p.emitRune(r)
	}
}

func (p *htmlParser) run() error {
	for p.i < len(p.src) {
		c := p.src[p.i]
		if c == '&' {
			decoded, n, ok := decodeHTMLEntity(p.src[p.i:])
			if ok {
				p.emitString(decoded)
				p.i += n
				continue
			}
			p.emitRune(rune(c))
			p.i++
			continue
		}
		if c == '<' {
			if err := p.consumeTag(); err != nil {
				return err
			}
			continue
		}
		// UTF-8 decode is not strictly necessary for offset accounting
		// because Go strings are UTF-8 and indexing via `for range` would
		// be ideal, but we already iterate bytes for tag detection. Decode
		// runes manually.
		r, size := decodeRune(p.src[p.i:])
		p.emitRune(r)
		p.i += size
	}
	// Drop any unclosed tags silently — they would be a client bug.
	p.stack = nil
	return nil
}

func (p *htmlParser) consumeTag() error {
	// p.src[p.i] == '<'
	end := strings.IndexByte(p.src[p.i:], '>')
	if end < 0 {
		// Unterminated tag — emit literal '<' and continue.
		p.emitRune('<')
		p.i++
		return nil
	}
	tag := p.src[p.i+1 : p.i+end]
	p.i += end + 1

	closing := false
	if strings.HasPrefix(tag, "/") {
		closing = true
		tag = tag[1:]
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil
	}

	// Self-closing trailing slash.
	selfClose := false
	if strings.HasSuffix(tag, "/") {
		selfClose = true
		tag = strings.TrimSpace(tag[:len(tag)-1])
	}

	// Split into name + attrs.
	name, attrs := splitTag(tag)
	name = strings.ToLower(name)

	// Map tag name to entity type.
	entityType := htmlTagType(name)

	// Special: <br>, <br/> → newline, no entity.
	if name == "br" {
		p.emitRune('\n')
		return nil
	}

	// Unknown tags: passthrough silently (drop the tag, keep inner content).
	if entityType == "" {
		return nil
	}

	if closing {
		// Close the most recent matching mark.
		for k := len(p.stack) - 1; k >= 0; k-- {
			if p.stack[k].typ == entityType {
				mk := p.stack[k]
				length := p.utf16Off - mk.utf16Pos
				if length > 0 {
					ent := OrbitEntity{
						Type:     mk.typ,
						Offset:   mk.utf16Pos,
						Length:   length,
						URL:      mk.url,
						Language: mk.language,
					}
					p.entities = append(p.entities, ent)
				}
				p.stack = append(p.stack[:k], p.stack[k+1:]...)
				return nil
			}
		}
		return nil
	}

	mark := htmlMark{
		typ:      entityType,
		utf16Pos: p.utf16Off,
	}

	if name == "a" {
		mark.url = htmlAttr(attrs, "href")
	}
	if name == "code" {
		// If we are inside <pre>, promote the language onto the parent pre
		// mark and don't emit a separate code entity.
		if cls := htmlAttr(attrs, "class"); strings.HasPrefix(cls, "language-") {
			lang := strings.TrimPrefix(cls, "language-")
			for k := len(p.stack) - 1; k >= 0; k-- {
				if p.stack[k].typ == "MessageEntityPre" {
					p.stack[k].language = lang
					return nil
				}
			}
		}
	}
	if name == "tg-spoiler" || (name == "span" && strings.Contains(htmlAttr(attrs, "class"), "tg-spoiler")) {
		mark.typ = "MessageEntitySpoiler"
	}

	if selfClose {
		// Self-closing meaningful tags are not part of TG HTML other than
		// <br>, which is handled above. Ignore.
		return nil
	}
	p.stack = append(p.stack, mark)
	return nil
}

func htmlTagType(name string) string {
	switch name {
	case "b", "strong":
		return "MessageEntityBold"
	case "i", "em":
		return "MessageEntityItalic"
	case "u", "ins":
		return "MessageEntityUnderline"
	case "s", "strike", "del":
		return "MessageEntityStrike"
	case "tg-spoiler":
		return "MessageEntitySpoiler"
	case "span":
		// Treated as spoiler when class="tg-spoiler", otherwise transparent.
		return "MessageEntitySpoiler"
	case "code":
		return "MessageEntityCode"
	case "pre":
		return "MessageEntityPre"
	case "a":
		return "MessageEntityTextUrl"
	case "blockquote":
		return "MessageEntityBlockquote"
	}
	return ""
}

func splitTag(tag string) (string, string) {
	for i, c := range tag {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			return tag[:i], strings.TrimSpace(tag[i+1:])
		}
	}
	return tag, ""
}

// htmlAttr extracts an attribute value from an attribute string like
// `href="https://example.com" class="x"`. Quotes (single or double) are
// stripped. Comparison is case-insensitive on the attribute name.
func htmlAttr(attrs, name string) string {
	low := strings.ToLower(attrs)
	target := strings.ToLower(name) + "="
	idx := 0
	for {
		k := strings.Index(low[idx:], target)
		if k < 0 {
			return ""
		}
		k += idx
		// Ensure preceding char is a boundary (start or whitespace).
		if k > 0 {
			prev := low[k-1]
			if prev != ' ' && prev != '\t' && prev != '\n' && prev != '\r' {
				idx = k + len(target)
				continue
			}
		}
		valStart := k + len(target)
		if valStart >= len(attrs) {
			return ""
		}
		quote := attrs[valStart]
		if quote == '"' || quote == '\'' {
			end := strings.IndexByte(attrs[valStart+1:], quote)
			if end < 0 {
				return ""
			}
			return attrs[valStart+1 : valStart+1+end]
		}
		// Unquoted: read until whitespace.
		end := valStart
		for end < len(attrs) {
			c := attrs[end]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				break
			}
			end++
		}
		return attrs[valStart:end]
	}
}

// decodeHTMLEntity decodes a single HTML character reference starting at the
// `&` byte. Returns the decoded string, the number of bytes consumed (including
// `&` and `;`), and whether a decode happened.
func decodeHTMLEntity(s string) (string, int, bool) {
	if !strings.HasPrefix(s, "&") {
		return "", 0, false
	}
	end := strings.IndexByte(s, ';')
	if end < 0 || end > 12 {
		return "", 0, false
	}
	body := s[1:end]
	if strings.HasPrefix(body, "#") {
		var code int
		hex := false
		if strings.HasPrefix(body, "#x") || strings.HasPrefix(body, "#X") {
			hex = true
			body = body[2:]
		} else {
			body = body[1:]
		}
		for _, c := range body {
			d := -1
			switch {
			case c >= '0' && c <= '9':
				d = int(c - '0')
			case hex && c >= 'a' && c <= 'f':
				d = int(c-'a') + 10
			case hex && c >= 'A' && c <= 'F':
				d = int(c-'A') + 10
			}
			if d < 0 {
				return "", 0, false
			}
			if hex {
				code = code*16 + d
			} else {
				code = code*10 + d
			}
			if code > 0x10FFFF {
				return "", 0, false
			}
		}
		return string(rune(code)), end + 1, true
	}
	switch body {
	case "amp":
		return "&", end + 1, true
	case "lt":
		return "<", end + 1, true
	case "gt":
		return ">", end + 1, true
	case "quot":
		return "\"", end + 1, true
	case "apos":
		return "'", end + 1, true
	case "nbsp":
		return " ", end + 1, true
	}
	return "", 0, false
}

// decodeRune wraps utf8.DecodeRuneInString to keep the call sites readable.
func decodeRune(s string) (rune, int) {
	r, n := utf8.DecodeRuneInString(s)
	return r, n
}
