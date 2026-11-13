package where_test

import (
	"os"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func TestMain(m *testing.M) {
	cleanup := config.IsolateHomeForTest()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
