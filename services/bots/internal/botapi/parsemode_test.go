// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"reflect"
	"testing"
)

func TestUTF16Len(t *testing.T) {
	cases := map[string]int{
		"":      0,
		"abc":   3,
		"héllo": 5,
		"🚀":     2, // U+1F680 → surrogate pair
		"a🚀b":   4,
	}
	for in, want := range cases {
		if got := utf16Len(in); got != want {
			t.Fatalf("utf16Len(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseMarkdownV2_BoldAndCode(t *testing.T) {
	text, ents, err := parseMarkdownV2("*bold* and `code`")
	if err != nil {
		t.Fatal(err)
	}
	if text != "bold and code" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityBold", Offset: 0, Length: 4},
		{Type: "MessageEntityCode", Offset: 9, Length: 4},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestParseMarkdownV2_ItalicAndUnderlineDistinguished(t *testing.T) {
	text, ents, err := parseMarkdownV2("_italic_ __under__")
	if err != nil {
		t.Fatal(err)
	}
	if text != "italic under" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityItalic", Offset: 0, Length: 6},
		{Type: "MessageEntityUnderline", Offset: 7, Length: 5},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestParseMarkdownV2_StrikeAndSpoiler(t *testing.T) {
	text, ents, err := parseMarkdownV2("~old~ ||secret||")
	if err != nil {
		t.Fatal(err)
	}
	if text != "old secret" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityStrike", Offset: 0, Length: 3},
		{Type: "MessageEntitySpoiler", Offset: 4, Length: 6},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestParseMarkdownV2_PreBlockWithLanguage(t *testing.T) {
	text, ents, err := parseMarkdownV2("```python\nprint(1)\n```")
	if err != nil {
		t.Fatal(err)
	}
	if text != "print(1)\n" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityPre", Offset: 0, Length: utf16Len("print(1)\n"), Language: "python"},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestParseMarkdownV2_TextLink(t *testing.T) {
	text, ents, err := parseMarkdownV2("see [docs](https://example.com/x)")
	if err != nil {
		t.Fatal(err)
	}
	if text != "see docs" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityTextUrl", Offset: 4, Length: 4, URL: "https://example.com/x"},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestParseMarkdownV2_Blockquote(t *testing.T) {
	text, ents, err := parseMarkdownV2(">quoted line")
	if err != nil {
		t.Fatal(err)
	}
	if text != "quoted line" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityBlockquote", Offset: 0, Length: 11},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestParseMarkdownV2_BackslashEscape(t *testing.T) {
	text, ents, err := parseMarkdownV2(`\*not bold\*`)
	if err != nil {
		t.Fatal(err)
	}
	if text != "*not bold*" {
		t.Fatalf("text=%q", text)
	}
	if len(ents) != 0 {
		t.Fatalf("expected no entities, got %+v", ents)
	}
}

func TestParseMarkdownV2_EmojiOffsetUTF16(t *testing.T) {
	// 🚀 occupies 2 UTF-16 code units; the bold entity that follows must
	// start at offset 2.
	text, ents, err := parseMarkdownV2("🚀*go*")
	if err != nil {
		t.Fatal(err)
	}
	if text != "🚀go" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityBold", Offset: 2, Length: 2},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestParseHTML_BoldItalicCode(t *testing.T) {
	text, ents, err := parseHTML("<b>Alert</b> <code>500</code>")
	if err != nil {
		t.Fatal(err)
	}
	if text != "Alert 500" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityBold", Offset: 0, Length: 5},
		{Type: "MessageEntityCode", Offset: 6, Length: 3},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestParseHTML_PreWithLanguage(t *testing.T) {
	text, ents, err := parseHTML(`<pre><code class="language-go">x := 1
</code></pre>`)
	if err != nil {
		t.Fatal(err)
	}
	if text != "x := 1\n" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityPre", Offset: 0, Length: utf16Len("x := 1\n"), Language: "go"},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestParseHTML_AnchorTextLink(t *testing.T) {
	text, ents, err := parseHTML(`<a href="https://example.com">link</a>`)
	if err != nil {
		t.Fatal(err)
	}
	if text != "link" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityTextUrl", Offset: 0, Length: 4, URL: "https://example.com"},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestParseHTML_DecodesEntities(t *testing.T) {
	text, _, err := parseHTML("&lt;tag&gt; &amp; &quot;quoted&quot; &#65;")
	if err != nil {
		t.Fatal(err)
	}
	if text != `<tag> & "quoted" A` {
		t.Fatalf("text=%q", text)
	}
}

func TestParseHTML_Blockquote(t *testing.T) {
	text, ents, err := parseHTML("<blockquote>hello</blockquote>")
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" {
		t.Fatalf("text=%q", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityBlockquote", Offset: 0, Length: 5},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestResolveTextAndEntities_PlainText(t *testing.T) {
	text, ents, err := resolveTextAndEntities("hello world", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello world" {
		t.Fatalf("text=%q", text)
	}
	if ents != nil {
		t.Fatalf("expected nil entities, got %+v", ents)
	}
}

func TestResolveTextAndEntities_InvalidParseMode(t *testing.T) {
	_, _, err := resolveTextAndEntities("hi", "Markdown", nil)
	if err == nil {
		t.Fatal("expected error for legacy 'Markdown' parse_mode")
	}
}

func TestResolveTextAndEntities_ExplicitOverridesParseMode(t *testing.T) {
	// parse_mode would yield bold "x" but explicit entities point to
	// italic "x" — explicit wins, text is unchanged (no marker stripping).
	explicit := []MessageEntity{
		{Type: "italic", Offset: 0, Length: 5},
	}
	text, ents, err := resolveTextAndEntities("*hello", ParseModeMarkdownV2, explicit)
	if err != nil {
		t.Fatal(err)
	}
	if text != "*hello" {
		t.Fatalf("text=%q (parse_mode should be skipped when explicit entities are present)", text)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityItalic", Offset: 0, Length: 5},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities=%+v, want %+v", ents, want)
	}
}

func TestConvertTGEntities_DropsUnknownTypes(t *testing.T) {
	in := []MessageEntity{
		{Type: "bold", Offset: 0, Length: 1},
		{Type: "weather_forecast", Offset: 5, Length: 2},
		{Type: "text_link", Offset: 10, Length: 3, URL: "https://x.test"},
	}
	out := convertTGEntities(in)
	want := []OrbitEntity{
		{Type: "MessageEntityBold", Offset: 0, Length: 1},
		{Type: "MessageEntityTextUrl", Offset: 10, Length: 3, URL: "https://x.test"},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("entities=%+v, want %+v", out, want)
	}
}
