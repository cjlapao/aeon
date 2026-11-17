//go:build noretrieval

package main

// retrieval_link_off.go — the no-retrieval configuration (S22 T6, V13).
//
// The no-op IS the design: with this file compiled, nothing anywhere can
// wire the resolver seam, so the `search` tool is not in the registry and
// `search.query` is not a registered method — absent, not present-and-
// empty, not registered-and-refusing (D1, D2, V21). There is deliberately
// no error to return: the build configuration is the refusal, and it was
// spoken long ago at the Makefile tag.

import (
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func wireRetrievalSearch(_ *store.Store, _ protocol.Routes, _ *daemon.Options) {}
