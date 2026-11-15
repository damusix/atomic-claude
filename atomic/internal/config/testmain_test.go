package config

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	cleanup := IsolateHomeForTest()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
