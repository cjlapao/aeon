// missingdo.go is the machine pin of C16's structural rule. It lives in a
// testdata directory, so `go build ./...` and `go test ./...` skip it
// (a non-compiling file in a real package would break the whole suite,
// which is the wrong signal), but TestErrorMissingDoDoesNotCompile runs
// `go build` on it explicitly and asserts the build fails — the same shape
// S11's forged-token test used (D11, C6): the rule is the compiler, not a
// convention a reader might forget.
//
// A struct literal that names two of the three fields must not compile,
// because the fields are unexported and the only constructor is New, which
// takes all three. If this ever compiles, the third part is optional in
// practice and C16 is a convention again.
package missingdo

import "github.com/cjlapao/aeon/internal/errs"

func build() {
	// This line must fail to compile: it names What and Why but not Do,
	// and the fields are unexported, so no literal in another package can
	// construct an Error without all three.
	_ = errs.Error{What: "x", Why: "y"}
}
