package overexported

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Chdir("testdata/foo")

	got, err := Run([]string{"./..."})
	require.NoError(t, err)
	require.Len(t, got.Exports, 1)
}
