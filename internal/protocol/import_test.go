package protocol

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// I1 · D11 · T11 — this package imports nothing transport-related.
//
// # Why this test is the most important one in the slice
//
// It is the invariant whose breach is INVISIBLE. Import `net` here and
// everything still works: the daemon serves, the tests pass, the demo walks.
// The cost arrives at S19, when the same method table has to be mounted on HTTP
// and MCP-stdio and cannot be, because the protocol package drags a unix
// listener with it. By then the fix is a refactor of fifteen slices' worth of
// handlers instead of a line nobody wrote.
//
// # Why it parses imports instead of grepping
//
// The task says "asserted by grep". Grep over source text answers a different
// question: it matches the word `Conn` in a comment, in `ConnectionState`, and
// in this very file — so it either cries wolf or gets weakened with exclusions
// until it matches nothing. Parsing the import list answers the actual
// question, and the identifier scan below keeps the part grep was right about:
// a transport reached through a dot-import or a local alias.
var forbiddenImports = []string{
	"net",
	"net/http",
	"net/rpc",
	"os/exec",
	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc",
}

func TestProtocolImportsNothingTransportRelated(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no source files found; this test would pass vacuously")
	}

	fset := token.NewFileSet()
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbiddenImports {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %q — internal/protocol must know nothing about "+
						"transports (T11), or the same table cannot be mounted on HTTP and "+
						"stdio at S19", file, path)
				}
			}
			// A package whose path contains these is a transport by another
			// name. Checked by substring because the next one will not be on
			// the list above.
			for _, hint := range []string{"localrpc", "websocket", "grpc"} {
				if strings.Contains(path, hint) {
					t.Errorf("%s imports %q, which looks like a transport", file, path)
				}
			}
		}
	}
}

// The part grep was right about: a transport reached without naming its import
// path, via a dot-import or an alias.
func TestNoTransportIdentifiersInProtocolSource(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	if len(files) == 0 {
		t.Fatal("no source files found; this test would pass vacuously")
	}
	for _, file := range files {
		if file == "import_test.go" {
			continue // names them all, by definition
		}
		ids := identifiersIn(t, file)
		// Both the qualified forms and the bare ones: a dot-import of `net`
		// would write `Listen(...)` with no package name to match on.
		for _, bad := range []string{
			"net.Listen", "net.Listener", "net.Dial", "net.Conn", "net.Pipe",
			"Listen", "ListenUnix", "Dial", "DialUnix",
		} {
			if usesIdentifier(ids, bad) {
				t.Errorf("%s uses %s in code (T11)", file, bad)
			}
		}
	}
}
