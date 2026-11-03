package alive

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The website compares only functions named @src and @tgt, so a src/tgt pair
// of modules is checked one function at a time, each merged into one module.

const fnName = `@("(?:[^"\\]|\\.)*"|[-\w$.]+)\s*\(`

var (
	defineRE   = regexp.MustCompile(`(?ms)^define\b[^\n]*?` + fnName + `.*?^}`)
	nameRE     = regexp.MustCompile(fnName)
	attrRef    = regexp.MustCompile(`#(\d+)\b`)
	mdRef      = regexp.MustCompile(`!(\d+)\b`)
	mdNumbered = regexp.MustCompile(`^!\d+ = `)
)

// renumber is added to tgt's attribute group and metadata numbers so they
// can't collide with src's in the merged module.
const renumber = 9000

type function struct{ name, text string }

func functions(ir string) []function {
	var out []function
	for _, m := range defineRE.FindAllStringSubmatch(ir, -1) {
		out = append(out, function{m[1], m[0]})
	}
	return out
}

func find(fns []function, name string) (string, bool) {
	for _, f := range fns {
		if f.name == name {
			return f.text, true
		}
	}
	return "", false
}

// SharedFunctions lists the functions defined in both modules, in src order.
func SharedFunctions(src, tgt string) []string {
	t := functions(tgt)
	var names []string
	for _, f := range functions(src) {
		if _, ok := find(t, f.name); ok {
			names = append(names, f.name)
		}
	}
	return names
}

func renameFirst(fn, to string) string {
	loc := nameRE.FindStringIndex(fn)
	if loc == nil {
		return fn
	}
	return fn[:loc[0]] + "@" + to + "(" + fn[loc[1]:]
}

func shift(re *regexp.Regexp, sigil, s string) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(m[len(sigil):])
		return sigil + strconv.Itoa(n+renumber)
	})
}

// Pair builds the module the website compares: src's module with function
// name renamed @src, followed by tgt's version renamed @tgt along with tgt's
// attribute groups and numbered metadata, renumbered.
func Pair(src, tgt, name string) (string, error) {
	s, ok1 := find(functions(src), name)
	t, ok2 := find(functions(tgt), name)
	if !ok1 || !ok2 {
		return "", fmt.Errorf("@%s is not defined in both src and tgt", name)
	}
	t = renameFirst(shift(mdRef, "!", shift(attrRef, "#", t)), "tgt")
	var b strings.Builder
	b.WriteString(strings.TrimRight(strings.Replace(src, s, renameFirst(s, "src"), 1), "\n"))
	b.WriteString("\n\n")
	b.WriteString(t)
	b.WriteString("\n")
	for _, line := range strings.Split(tgt, "\n") {
		switch {
		case strings.HasPrefix(line, "attributes #"):
			b.WriteString(shift(attrRef, "#", line) + "\n")
		case mdNumbered.MatchString(line):
			b.WriteString(shift(mdRef, "!", line) + "\n")
		}
	}
	return b.String(), nil
}
