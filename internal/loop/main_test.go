package loop

import (
	"os"
	"testing"

	"github.com/cjlapao/aeon/internal/testutil/testenv"
	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	// Hundreds of real migration fixtures must not resolve editable blueprints
	// from user state, or print every successful migration into the test output.
	logrus.SetLevel(logrus.WarnLevel)
	os.Exit(testenv.Run(m))
}
