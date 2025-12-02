package overexported

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkRun_ExternalTest(b *testing.B) {
	var err error
	var got *Result

	b.ReportAllocs()
	for b.Loop() {
		got, err = Run([]string{"./..."}, &Options{Test: true, Dir: "testdata/external_test"})
		if err != nil {
			break
		}
	}
	require.NoError(b, err)
	require.NotNil(b, got)
}
