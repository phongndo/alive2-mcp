package alive

import (
	"reflect"
	"strings"
	"testing"
)

func TestFunctions(t *testing.T) {
	ir := `declare i8 @ext(i8)
define i8 @a(i8 %x) {
  ret i8 %x
}
define internal void @"quoted name"() {
  ret void
}
define i32 @b.c$d(ptr %p) #0 {
entry:
  ret i32 0
}`
	var names []string
	for _, f := range functions(ir) {
		names = append(names, f.name)
	}
	want := []string{"a", `"quoted name"`, "b.c$d"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("functions = %q, want %q", names, want)
	}
}

func TestSharedFunctions(t *testing.T) {
	got := SharedFunctions(fixture(t, "pair-src.ll"), fixture(t, "pair-tgt.ll"))
	if !reflect.DeepEqual(got, []string{"f", "h"}) {
		t.Errorf("SharedFunctions = %v", got)
	}
}

func TestPair(t *testing.T) {
	m, err := Pair(fixture(t, "pair-src.ll"), fixture(t, "pair-tgt.ll"), "f")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"define i8 @src(ptr %p) #0 {",    // src keeps its own numbering
		"define i8 @tgt(ptr %p) #9001 {", // tgt's attribute group renumbered
		"!range !9003",                   // and its metadata
		"attributes #9001 = { nounwind willreturn }",
		"!9003 = !{i8 0, i8 10}",
		"define i8 @h(i8 %x)", // other src functions stay
		"declare i8 @g(i8)",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("pair module lacks %q:\n%s", want, m)
		}
	}
	if strings.Count(m, "@tgt(") != 1 || strings.Count(m, "define i8 @src(") != 1 {
		t.Errorf("expected exactly one @src and one @tgt:\n%s", m)
	}
	if _, err := Pair("define void @x() {\n}", "", "x"); err == nil {
		t.Error("Pair should fail when tgt lacks the function")
	}
}
