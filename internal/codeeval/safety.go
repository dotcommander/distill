package codeeval

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// ScanResult reports whether parsed model code is safe to compile+run under the
// non-sandboxed default runner. Defense-in-depth, NOT a sandbox: it blocks the
// obvious dangerous-import / dangerous-call classes a benign solver never needs;
// it does NOT stop a determined adversary.
type ScanResult struct {
	Safe    bool
	Reasons []string
}

var bannedImports = map[string]bool{
	"os/exec":   true,
	"net":       true,
	"net/http":  true,
	"syscall":   true,
	"plugin":    true,
	"unsafe":    true,
	"os/signal": true,
}

// bannedCalls maps a package identifier to its banned function names.
var bannedCalls = map[string]map[string]bool{
	"os": {"RemoveAll": true, "Remove": true, "OpenFile": true, "Create": true, "WriteFile": true, "Rename": true},
}

// ScanCode parses src as Go and flags banned imports and banned pkg.Fn calls. A
// parse error is NOT a safety failure (it is a compile failure, scored
// separately) — return Safe:true so the runner reaches the compile step.
func ScanCode(src string) ScanResult {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "solution.go", src, parser.AllErrors)
	if err != nil {
		return ScanResult{Safe: true}
	}
	var reasons []string
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if bannedImports[path] {
			reasons = append(reasons, "banned import: "+path)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if fns, ok := bannedCalls[pkgIdent.Name]; ok && fns[sel.Sel.Name] {
			reasons = append(reasons, "banned call: "+pkgIdent.Name+"."+sel.Sel.Name)
		}
		return true
	})
	return ScanResult{Safe: len(reasons) == 0, Reasons: reasons}
}
